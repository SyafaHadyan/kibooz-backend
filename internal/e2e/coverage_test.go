package e2e

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/stretchr/testify/require"
)

func allOperations(t *testing.T) map[string]bool {
	t.Helper()

	all := map[string]bool{}

	for path, item := range spec(t).Paths.Map() {
		for method := range item.Operations() {
			all[operationKey(method, path)] = true
		}
	}

	require.NotEmpty(t, all)

	return all
}

func TestEveryOperationBeingExercisedLeavesNothingMissing(t *testing.T) {
	require.Empty(t, unexercised(spec(t), allOperations(t)))
}

func TestAnOperationThatWasNeverExercisedIsReported(t *testing.T) {
	seen := allOperations(t)

	delete(seen, "POST /auth/login")
	delete(seen, "GET /healthz")

	require.Equal(t, []string{"GET /healthz", "POST /auth/login"}, unexercised(spec(t), seen))
}

func TestNothingExercisedReportsEveryOperationInOrder(t *testing.T) {
	missing := unexercised(spec(t), map[string]bool{})

	require.Equal(t, len(allOperations(t)), len(missing))
	require.True(t, slices.IsSorted(missing))
}

func TestAnOperationOutsideTheSpecIsNotReported(t *testing.T) {
	seen := allOperations(t)
	seen["GET /not-documented"] = true

	require.Empty(t, unexercised(spec(t), seen))
}

func TestAConcretePathIsMatchedToItsTemplate(t *testing.T) {
	doc := spec(t)
	id := "6f1c1a52-0c55-4d6c-a4a9-8f54a39bd1a8"

	tests := map[string]string{
		"/guru/dashboard":                              "/guru/dashboard",
		"/classes/" + id + "/videos":                   "/classes/{classId}/videos",
		"/classes/" + id + "/forum":                    "/classes/{classId}/forum",
		"/classes/" + id + "/forum/" + id + "/replies": "/classes/{classId}/forum/{postId}/replies",
		"/classes/{classId}/videos":                    "/classes/{classId}/videos",
		"/classes/" + id + "/videos/" + id:             "/classes/{classId}/videos/{videoId}",
	}

	for concrete, want := range tests {
		got, item := findPath(doc, concrete)
		require.Equal(t, want, got, concrete)
		require.NotNil(t, item, concrete)
	}
}

func TestAPathOutsideTheSpecIsNotMatched(t *testing.T) {
	doc := spec(t)

	for _, path := range []string{"/classes//videos", "/classes/x/videos/extra/more", "/classes/x", "/not-documented", ""} {
		got, item := findPath(doc, path)
		require.Empty(t, got, path)
		require.Nil(t, item, path)
	}
}

func TestADocumentedPathUsesTheParameterNamesOfFiber(t *testing.T) {
	require.Equal(t, "/healthz", documentedPath("/healthz"))
	require.Equal(t, "/api/v1/guru/dashboard", documentedPath("/guru/dashboard"))
	require.Equal(t, "/api/v1/classes/:classId/forum/:postId/replies", documentedPath("/classes/{classId}/forum/{postId}/replies"))
}

// Several templates match this path, and the one that was picked used to depend on the order of a map
func TestAFixedSegmentWinsOverAParameter(t *testing.T) {
	doc := spec(t)
	id := "6f1c1a52-0c55-4d6c-a4a9-8f54a39bd1a8"

	for range 200 {
		got, item := findPath(doc, "/classes/"+id+"/videos/upload-url")

		require.Equal(t, "/classes/{classId}/videos/upload-url", got)
		require.NotNil(t, item.Post)
	}

	got, _ := findPath(doc, "/classes/"+id+"/videos/"+id)
	require.Equal(t, "/classes/{classId}/videos/{videoId}", got)
}

func TestAnOperationRefusedOnlyIsStillMissingItsSuccessResponse(t *testing.T) {
	seen := map[string]bool{}

	for _, key := range unexercisedSuccess(spec(t), map[string]bool{}) {
		seen[key] = true
	}

	require.NotEmpty(t, seen)
	require.Contains(t, seen, "GET /healthz 200")
	require.NotContains(t, seen, "GET /healthz 503", "only success responses are required")

	seen = map[string]bool{"GET /healthz 200": true, "POST /auth/login 400": true}

	missing := unexercisedSuccess(spec(t), seen)
	require.NotContains(t, missing, "GET /healthz 200")
	require.Contains(t, missing, "POST /auth/login 200", "a 400 does not stand in for the 200")
	require.True(t, slices.IsSorted(missing))
}

func validateHealth(t *testing.T, body string) error {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	route := routeFor(spec(t), http.MethodGet, "/healthz")
	require.NotNil(t, route)

	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, Route: route},
		Status:                 http.StatusOK,
		Header:                 http.Header{"Content-Type": []string{"application/json"}},
		Options:                &openapi3filter.Options{IncludeResponseStatus: true},
	}
	input.SetBodyBytes([]byte(body))

	return openapi3filter.ValidateResponse(t.Context(), input)
}

// The validator accepts a field that the spec does not list unless the schema forbids it
func TestAResponseWithAFieldThatIsNotDocumentedFails(t *testing.T) {
	const healthy = `{"success":true,"status":"ok","version":"1","checks":{"database":"ok","redis":"ok","storage":"disabled"}}`

	require.NoError(t, validateHealth(t, healthy))

	require.Error(t, validateHealth(t, `{"success":true,"status":"ok","version":"1","passwordHash":"x","checks":{"database":"ok","redis":"ok","storage":"disabled"}}`),
		"a field next to the documented ones")
	require.Error(t, validateHealth(t, `{"success":true,"status":"ok","version":"1","checks":{"database":"ok","redis":"ok","storage":"disabled","secret":"x"}}`),
		"a field inside a nested object")
}

// A list is the page fields plus its own, so the schema that combines them takes both and the parts stay open
func TestACombinedSchemaTakesTheFieldsOfItsParts(t *testing.T) {
	schemas := spec(t).Components.Schemas

	list := schemas["VideoList"].Value
	require.NotNil(t, list.AdditionalProperties.Has)
	require.False(t, *list.AdditionalProperties.Has)
	require.Contains(t, list.Properties, "videos")

	for _, name := range []string{"page", "limit", "total"} {
		require.Contains(t, list.Properties, name)
	}

	part := schemas["PageInfo"].Value
	require.Nil(t, part.AdditionalProperties.Has, "a part of allOf has to stay open or the fields of the others are refused")
}

// details names its fields freely, so its own rule about the values has to stay
func TestASchemaThatSaysOtherwiseIsNotChanged(t *testing.T) {
	details := spec(t).Components.Schemas["Failure"].Value.Properties["details"].Value

	require.NotNil(t, details.AdditionalProperties.Schema)
	require.Nil(t, details.AdditionalProperties.Has)
}
