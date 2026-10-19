package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTheBatchMoodRouteHasALimitOfItsOwn(t *testing.T) {
	guru := registerGuru(t, "Batas Batch")
	classID, _ := classOf(t, guru)

	// an empty list is refused after the limiter has counted the request, which keeps this test from writing 30 batches
	empty := func() result {
		return call(t, http.MethodPost, moodBatchPath, guru.Token, map[string]any{"classId": classID, "entries": []map[string]any{}})
	}

	for i := 1; i <= 30; i++ {
		res := empty()
		require.Equal(t, http.StatusBadRequest, res.Status, "request %d body %v", i, res.Body)
	}

	empty().requireError(t, http.StatusTooManyRequests, "RATE_LIMITED")

	t.Run("the other mood route and other users are not affected", func(t *testing.T) {
		_, joinCode := classOf(t, guru)
		_, studentID := registerWali(t, joinCode, "Anak Batch")
		require.Equal(t, http.StatusCreated, call(t, http.MethodPost, moodLogRoute, guru.Token, map[string]any{"studentId": studentID, "moodType": "SENANG"}).Status)

		other := registerGuru(t, "Batas Lain")
		otherClass, _ := classOf(t, other)
		res := call(t, http.MethodPost, moodBatchPath, other.Token, map[string]any{"classId": otherClass, "entries": []map[string]any{}})
		require.Equal(t, http.StatusBadRequest, res.Status)
	})
}
