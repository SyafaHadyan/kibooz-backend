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
