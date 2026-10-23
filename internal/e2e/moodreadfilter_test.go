package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A child that never moved has no earlier class, so a mood that the write side accepted stays visible even when it is dated
// a little before the child was added, for example by a phone whose clock is behind.
func TestAMoodDatedBeforeAChildWasAddedStillShowsForAChildThatNeverMoved(t *testing.T) {
	guru := registerGuru(t, "Mundur Satu")
	classID, code := classOf(t, guru)
	_, childID := registerWali(t, code, "Anak Mundur")

	var createdAt time.Time

	require.NoError(t, testDB.Raw("SELECT created_at FROM students WHERE id = ?::uuid", childID).Scan(&createdAt).Error)

	early := createdAt.Add(-time.Second).UTC().Format(time.RFC3339Nano)

	logged := call(t, http.MethodPost, moodLogRoute, guru.Token, map[string]any{"studentId": childID, "moodType": "SENANG", "recordedAt": early})
	require.Equal(t, http.StatusCreated, logged.Status, "body %v", logged.Body)

	roster := call(t, http.MethodGet, "/api/v1/classes/"+classID+"/students", guru.Token, nil)
	require.Equal(t, http.StatusOK, roster.Status, "body %v", roster.Body)

	found := false

	for _, row := range roster.data("students").([]any) {
		if dig(row, "id") == childID {
			found = true

			require.Equal(t, "SENANG", dig(row, "todayMood"), "the mood is dated before the child was added and still has to show")
		}
	}

	require.True(t, found, "the child has to be in the roster")
}
