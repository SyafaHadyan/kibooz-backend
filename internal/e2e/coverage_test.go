package e2e

import (
	"slices"
	"testing"

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
