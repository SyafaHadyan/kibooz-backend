package e2e

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
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
	// exercisedStatus holds the operation and the status of every response that was checked, such as "GET /healthz 200"
	exercisedStatus = map[string]bool{}
)

func loadSpec() (*openapi3.T, error) {
	specOnce.Do(func() {
		doc, err := openapi3.NewLoader().LoadFromFile(specFile)
		if err != nil {
			specErr = err

			return
		}

		specErr = doc.Validate(context.Background())

		forbidUnknownResponseFields(doc)

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

// unexercisedSuccess returns the success responses of the spec that no response was checked against. An operation that the
// tests only ever refuse, with a 400 or a 403, would otherwise count as exercised while its success body is never compared with the schema.
func unexercisedSuccess(doc *openapi3.T, seen map[string]bool) []string {
	var missing []string

	for path, item := range doc.Paths.Map() {
		for method, operation := range item.Operations() {
			if operation.Responses == nil {
				continue
			}

			for code := range operation.Responses.Map() {
				if strings.HasPrefix(code, "2") && !seen[operationKey(method, path)+" "+code] {
					missing = append(missing, operationKey(method, path)+" "+code)
				}
			}
		}
	}

	slices.Sort(missing)

	return missing
}

// subschemas returns the schemas inside a schema, the ones that describe a part of the same value (allOf, oneOf, anyOf) apart from the ones
// that describe a value inside it (a property, the items of a list or the values of a map)
func subschemas(schema *openapi3.Schema) (inside []*openapi3.Schema, parts []*openapi3.Schema) {
	for _, property := range schema.Properties {
		if property != nil && property.Value != nil {
			inside = append(inside, property.Value)
		}
	}

	if schema.Items != nil && schema.Items.Value != nil {
		inside = append(inside, schema.Items.Value)
	}

	if schema.AdditionalProperties.Schema != nil && schema.AdditionalProperties.Schema.Value != nil {
		inside = append(inside, schema.AdditionalProperties.Schema.Value)
	}

	for _, list := range []openapi3.SchemaRefs{schema.AllOf, schema.OneOf, schema.AnyOf} {
		for _, part := range list {
			if part != nil && part.Value != nil {
				parts = append(parts, part.Value)
			}
		}
	}

	return inside, parts
}

// forbidUnknownResponseFields makes every object in a documented response reject a field that the spec does not list.
// The validator accepts extra fields unless a schema says otherwise, so a handler that started returning a field such as a
// password hash would pass the contract check. A schema that is a part of an allOf, oneOf or anyOf is left open, because it
// only describes some of the fields of the value. The schema that combines them takes the fields of its allOf parts instead.
func forbidUnknownResponseFields(doc *openapi3.T) {
	var roots []*openapi3.Schema

	for _, item := range doc.Paths.Map() {
		for _, operation := range item.Operations() {
			if operation.Responses == nil {
				continue
			}

			for _, response := range operation.Responses.Map() {
				if response == nil || response.Value == nil {
					continue
				}

				for _, media := range response.Value.Content {
					if media.Schema != nil && media.Schema.Value != nil {
						roots = append(roots, media.Schema.Value)
					}
				}
			}
		}
	}

	composed := map[*openapi3.Schema]bool{}
	visited := map[*openapi3.Schema]bool{}

	for _, root := range roots {
		markComposed(root, composed, visited)
	}

	visited = map[*openapi3.Schema]bool{}

	for _, root := range roots {
		closeSchema(root, composed, visited)
	}
}

func markComposed(schema *openapi3.Schema, composed map[*openapi3.Schema]bool, visited map[*openapi3.Schema]bool) {
	if visited[schema] {
		return
	}

	visited[schema] = true

	inside, parts := subschemas(schema)

	for _, part := range parts {
		composed[part] = true

		markComposed(part, composed, visited)
	}

	for _, value := range inside {
		markComposed(value, composed, visited)
	}
}

// allProperties returns the properties of a schema together with the ones of its allOf parts
func allProperties(schema *openapi3.Schema) openapi3.Schemas {
	all := openapi3.Schemas{}

	for name, property := range schema.Properties {
		all[name] = property
	}

	for _, part := range schema.AllOf {
		if part == nil || part.Value == nil {
			continue
		}

		for name, property := range allProperties(part.Value) {
			all[name] = property
		}
	}

	return all
}

func closeSchema(schema *openapi3.Schema, composed map[*openapi3.Schema]bool, visited map[*openapi3.Schema]bool) {
	if visited[schema] {
		return
	}

	visited[schema] = true

	inside, parts := subschemas(schema)

	for _, value := range append(inside, parts...) {
		closeSchema(value, composed, visited)
	}

	if composed[schema] || schema.AdditionalProperties.Has != nil || schema.AdditionalProperties.Schema != nil ||
		len(schema.OneOf) > 0 || len(schema.AnyOf) > 0 {
		return
	}

	properties := allProperties(schema)
	if len(properties) == 0 {
		return
	}

	schema.Properties = properties

	forbidden := false
	schema.AdditionalProperties = openapi3.AdditionalProperties{Has: &forbidden}
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
		missingSuccess := unexercisedSuccess(doc, exercisedStatus)
		exercisedMu.Unlock()

		if len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "FAIL these operations of openapi.yaml were never exercised by the end to end tests\n  %s\n",
				strings.Join(missing, "\n  "))

			code = 1
		}

		if len(missingSuccess) > 0 {
			fmt.Fprintf(os.Stderr, "FAIL these success responses of openapi.yaml were never seen by the end to end tests\n  %s\n",
				strings.Join(missingSuccess, "\n  "))

			code = 1
		}
	}

	os.Exit(code)
}

// templateParam finds the {name} segments of a path in the spec
var templateParam = regexp.MustCompile(`\{([^}/]+)\}`)

// documentedPath turns a path of the spec into the path the server answers on, only /healthz lives outside the prefix.
// A {name} segment becomes the :name segment of Fiber, so the parameter names have to match the code as well.
func documentedPath(path string) string {
	if path == "/healthz" {
		return path
	}

	return apiPrefix + templateParam.ReplaceAllString(path, ":$1")
}

// findPath matches a path of a request against the paths of the spec, where a {name} segment accepts any value.
// It returns the path as the spec writes it, so every operation is counted under one name.
func findPath(doc *openapi3.T, path string) (string, *openapi3.PathItem) {
	if item := doc.Paths.Value(path); item != nil {
		return path, item
	}

	segments := strings.Split(path, "/")

	best, bestParameters := "", 0

	// Several templates can match, such as /videos/upload-url and /videos/{videoId}. The one with the fewest parameters is the
	// one the router picks, because a fixed segment is more specific, and the order of a map must never decide.
	for template := range doc.Paths.Map() {
		wanted := strings.Split(template, "/")
		if len(wanted) != len(segments) {
			continue
		}

		matched, parameters := true, 0

		for i := range wanted {
			parameter := strings.HasPrefix(wanted[i], "{") && strings.HasSuffix(wanted[i], "}")
			if parameter {
				parameters++
			}

			if (parameter && segments[i] == "") || (!parameter && wanted[i] != segments[i]) {
				matched = false

				break
			}
		}

		if matched && (best == "" || parameters < bestParameters || (parameters == bestParameters && template < best)) {
			best, bestParameters = template, parameters
		}
	}

	if best == "" {
		return "", nil
	}

	return best, doc.Paths.Value(best)
}

func routeFor(doc *openapi3.T, method string, path string) *routers.Route {
	specPath := path

	if path != "/healthz" {
		var found bool

		if specPath, found = strings.CutPrefix(path, apiPrefix); !found {
			return nil
		}
	}

	template, item := findPath(doc, specPath)
	if item == nil {
		return nil
	}

	operation := item.GetOperation(method)
	if operation == nil {
		return nil
	}

	return &routers.Route{Spec: doc, Path: template, PathItem: item, Method: method, Operation: operation}
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
	exercisedStatus[operationKey(route.Method, route.Path)+" "+strconv.Itoa(status)] = true
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
