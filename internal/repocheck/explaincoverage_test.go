package repocheck_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// explainVariables names the variable that internal/explain/explain_test.go gives the repository of each package under internal/app
var explainVariables = map[string]string{
	"auth": "auth", "chat": "chat", "classroom": "classroom", "guru": "guruRepo",
	"push": "pushRepo", "trash": "trash", "user": "userRepo", "wali": "waliRepo",
}

// noPlanNeeded lists the exported methods of a repository that the Explain plans test does not call, each with the reason. It starts
// empty, because every method of every repository is called there, and a method that is added without a call has to be called there
// or named here with a reason that a reviewer can accept, for example a single insert by primary key with no read before it.
var noPlanNeeded = map[string]string{}

// Every exported method of a repository runs a statement against the database, and the Explain plans workflow writes the plan of
// each one on a database of the size of production. A method that the test does not call has no plan, so a slow query or a missing
// index in it is found only in production. This test fails for such a method. It looks at the methods of the types whose name ends in
// DB under internal/app/*/repository. The packages with queries of their own, such as ranking and classteam, are reached through those
// methods, and a new package of that kind needs its own call in the Explain test.
func TestEveryRepositoryMethodHasAPlanInTheExplainTest(t *testing.T) {
	called := calledInExplainTest(t)

	files, err := filepath.Glob("../app/*/repository/*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)

	var missing []string

	found := map[string]bool{}

	covered := 0

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}

		pkg := filepath.Base(filepath.Dir(filepath.Dir(file)))
		variable, known := explainVariables[pkg]
		require.True(t, known, "the repository of %s has no variable in explainVariables, add it and call its methods in internal/explain", pkg)

		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		require.NoError(t, err)

		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || !function.Name.IsExported() || !strings.HasSuffix(receiverName(function), "DB") {
				continue
			}

			key := pkg + "." + function.Name.Name
			found[key] = true

			if reason, skipped := noPlanNeeded[key]; skipped {
				require.NotEmpty(t, reason, "%s is listed without a reason", key)

				continue
			}

			if called[variable+"."+function.Name.Name] {
				covered++

				continue
			}

			missing = append(missing, key)
		}
	}

	sort.Strings(missing)

	require.NotZero(t, covered, "no repository method was found, the test would pass without checking anything")
	require.Empty(t, missing, "these repository methods are not called in internal/explain/explain_test.go, so their queries have no plan")

	for key := range noPlanNeeded {
		require.True(t, found[key], "%s in noPlanNeeded is not an exported method of a repository, remove it", key)
	}
}

// calledInExplainTest lists the calls of the form variable.Method( in the Explain plans test, read from its syntax and not from its
// text, so a call in a comment or in a title does not count. The file has a build tag that only keeps it out of a normal build, and
// the parser reads it anyway.
func calledInExplainTest(t *testing.T) map[string]bool {
	t.Helper()

	parsed, err := parser.ParseFile(token.NewFileSet(), "../explain/explain_test.go", nil, 0)
	require.NoError(t, err)

	calls := map[string]bool{}

	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}

		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		if variable, ok := selector.X.(*ast.Ident); ok {
			calls[variable.Name+"."+selector.Sel.Name] = true
		}

		return true
	})

	return calls
}

// receiverName is the type name of the receiver of a method, without the pointer
func receiverName(function *ast.FuncDecl) string {
	switch expression := function.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if name, ok := expression.X.(*ast.Ident); ok {
			return name.Name
		}
	case *ast.Ident:
		return expression.Name
	}

	return ""
}
