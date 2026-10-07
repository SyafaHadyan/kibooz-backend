package e2e

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/stretchr/testify/require"
)

// The published documentation is built from openapi.yaml, so every response the tests see is checked against it.
// A field, status code or error response that the code changes without the spec fails the build.
const (
	specFile  = "../../openapi.yaml"
	apiPrefix = "/api/v1"
)

var (
	specOnce sync.Once
	specDoc  *openapi3.T
	specErr  error

	exercisedMu sync.Mutex
	exercised   = map[string]bool{}
)

func loadSpec() (*openapi3.T, error) {
	specOnce.Do(func() {
		doc, err := openapi3.NewLoader().LoadFromFile(specFile)
		if err != nil {
			specErr = err

			return
		}

		specErr = doc.Validate(context.Background())
		specDoc = doc
	})

	return specDoc, specErr
}

func spec(t *testing.T) *openapi3.T {
	t.Helper()

	doc, err := loadSpec()
	require.NoError(t, err)

	return doc
}

// operationKey names an operation the way the spec does, without the prefix of the server
func operationKey(method string, specPath string) string {
	return method + " " + specPath
}

// unexercised returns the operations of the spec that no response was checked against, in a stable order
func unexercised(doc *openapi3.T, seen map[string]bool) []string {
	var missing []string

	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			if !seen[operationKey(method, path)] {
				missing = append(missing, operationKey(method, path))
			}
		}
	}

	slices.Sort(missing)

	return missing
}

// wholeSuiteRan tells whether the guard can judge, which it cannot when the tests were filtered or skipped
func wholeSuiteRan() bool {
	if os.Getenv("E2E_ENABLED") != "true" || testing.Short() {
		return false
	}

	for _, name := range []string{"test.run", "test.skip"} {
		if f := flag.Lookup(name); f == nil || f.Value.String() != "" {
			return false
		}
	}

	return true
}

// TestMain fails the run when the whole suite passed and still left a documented operation without a single
// response checked against the spec. Such an operation would be documented without anything to keep it true.
func TestMain(m *testing.M) {
	code := m.Run()

	if code == 0 && wholeSuiteRan() {
		doc, err := loadSpec()
		if err != nil {
			fmt.Fprintln(os.Stderr, "load the spec for the coverage check", err)

			os.Exit(1)
		}

		exercisedMu.Lock()
		missing := unexercised(doc, exercised)
		exercisedMu.Unlock()

		if len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "FAIL these operations of openapi.yaml were never exercised by the end to end tests\n  %s\n",
				strings.Join(missing, "\n  "))

			code = 1
		}
	}

	os.Exit(code)
}

// documentedPath turns a path of the spec into the path the server answers on, only /healthz lives outside the prefix
func documentedPath(path string) string {
	if path == "/healthz" {
		return path
	}

	return apiPrefix + path
}

func routeFor(doc *openapi3.T, method string, path string) *routers.Route {
	specPath := path

	if path != "/healthz" {
		var found bool

		if specPath, found = strings.CutPrefix(path, apiPrefix); !found {
			return nil
		}
	}

	item := doc.Paths.Find(specPath)
	if item == nil {
		return nil
	}

	operation := item.GetOperation(method)
	if operation == nil {
		return nil
	}

	return &routers.Route{Spec: doc, Path: specPath, PathItem: item, Method: method, Operation: operation}
}

// requireContract checks one response. Requests for paths that are not documented, such as a probe for a missing route, are skipped.
func requireContract(t *testing.T, req *http.Request, status int, header http.Header, body []byte) {
	t.Helper()

	route := routeFor(spec(t), req.Method, req.URL.Path)
	if route == nil {
		return
	}

	exercisedMu.Lock()
	exercised[operationKey(route.Method, route.Path)] = true
	exercisedMu.Unlock()

	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, Route: route},
		Status:                 status,
		Header:                 header,
		Options:                &openapi3filter.Options{IncludeResponseStatus: true},
	}
	input.SetBodyBytes(body)

	require.NoError(t, openapi3filter.ValidateResponse(t.Context(), input),
		"%s %s answered %d and openapi.yaml does not describe it, body %s", req.Method, req.URL.Path, status, body)
}

// TestSpecMatchesRoutes fails when an endpoint exists in only one of the code and openapi.yaml
func TestSpecMatchesRoutes(t *testing.T) {
	doc := spec(t)

	documented := map[string]bool{}

	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			documented[method+" "+documentedPath(path)] = true
		}
	}

	served := map[string]bool{}

	for _, route := range app(t).GetRoutes(true) {
		// Fiber adds HEAD for every GET and the tests do not describe OPTIONS
		if !slices.Contains([]string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}, route.Method) {
			continue
		}

		served[route.Method+" "+route.Path] = true
	}

	var undocumented, unserved []string

	for route := range served {
		if !documented[route] {
			undocumented = append(undocumented, route)
		}
	}

	for route := range documented {
		if !served[route] {
			unserved = append(unserved, route)
		}
	}

	slices.Sort(undocumented)
	slices.Sort(unserved)

	require.Empty(t, undocumented, "these routes exist in the code but not in openapi.yaml")
	require.Empty(t, unserved, "these operations are in openapi.yaml but the code does not serve them")
}
