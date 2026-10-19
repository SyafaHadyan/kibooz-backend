package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAParentEditsAChild(t *testing.T) {
	guru := registerGuru(t, "Lavender Kecil")
	classID, code := classOf(t, guru)
	parent, childID := registerWali(t, code, "Budi Satu")
	other, otherChildID := registerWali(t, code, "Cici Dua")

	patch := func(token string, id string, body any) result {
		return call(t, http.MethodPatch, "/api/v1/wali/children/"+id, token, body)
	}

	nisnOf := func(token string, id string) string {
		res := call(t, http.MethodGet, "/api/v1/wali/child/"+id, token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		value, _ := res.data("nisn").(string)

		return value
	}

	rank := func(token string, id string) any {
		return call(t, http.MethodGet, "/api/v1/wali/dashboard?studentId="+id, token, nil).data("pointsSummary", "classRank")
	}

	require.NoError(t, testDB.Exec(
		"INSERT INTO leaderboard_history (class_id, student_id, student_name, season_label, points, rank_position) "+
			"VALUES (?::uuid, ?::uuid, 'Budi Satu', '2029-01', 40, 1), (?::uuid, ?::uuid, 'Cici Dua', '2029-01', 30, 2)",
		classID, childID, classID, otherChildID,
	).Error)

	historyName := func(id string) string {
		var names []string

		require.NoError(t, testDB.Raw("SELECT student_name FROM leaderboard_history WHERE student_id = ?::uuid", id).Scan(&names).Error)
		require.Len(t, names, 1)

		return names[0]
	}

	require.EqualValues(t, 1, rank(parent.Token, childID), "children with the same points are ordered by name")
	require.EqualValues(t, 2, rank(other.Token, otherChildID))

	t.Run("a new name moves the child in the ranking and into the old standings", func(t *testing.T) {
		res := patch(parent.Token, childID, map[string]any{"fullName": "  Zaki Terakhir  "})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, "Child updated", res.Body["message"])
		require.Equal(t, "Zaki Terakhir", res.data("fullName"))
		require.Equal(t, childID, res.data("id"))

		require.EqualValues(t, 1, rank(other.Token, otherChildID))
		require.EqualValues(t, 2, rank(parent.Token, childID))

		require.Equal(t, "Zaki Terakhir", historyName(childID))
		require.Equal(t, "Cici Dua", historyName(otherChildID), "the other child keeps its old standings")

		list := call(t, http.MethodGet, "/api/v1/wali/children", parent.Token, nil)
		require.Equal(t, "Zaki Terakhir", dig(listOf(t, list, "children")[0], "fullName"))
	})

	t.Run("a new NISN frees the old one", func(t *testing.T) {
		before := nisnOf(parent.Token, childID)
		fresh := nisn()

		res := patch(parent.Token, childID, map[string]any{"nisn": " " + fresh + " "})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, fresh, res.data("nisn"))
		require.Equal(t, "Zaki Terakhir", res.data("fullName"), "the name was not part of the change")
		require.Equal(t, fresh, nisnOf(parent.Token, childID))

		again := registerWaliWith(t, fmt.Sprintf("reuse.%s@example.com", suffix()), before, code, "Reuses The Old NISN")
		require.Equal(t, http.StatusCreated, again.Status, "body %v", again.Body)

		same := patch(parent.Token, childID, map[string]any{"nisn": fresh})
		require.Equal(t, http.StatusOK, same.Status, "naming the NISN the child has again changes nothing")
	})

	t.Run("a NISN of another active child is refused", func(t *testing.T) {
		taken := nisnOf(other.Token, otherChildID)
		current := nisnOf(parent.Token, childID)

		patch(parent.Token, childID, map[string]any{"nisn": taken, "fullName": "Should Not Stick"}).
			requireError(t, http.StatusConflict, "NISN_ALREADY_REGISTERED")

		require.Equal(t, current, nisnOf(parent.Token, childID))
		require.Equal(t, "Zaki Terakhir", call(t, http.MethodGet, "/api/v1/wali/child/"+childID, parent.Token, nil).data("fullName"),
			"the refused change did not change the name either")
	})

	t.Run("bad input is refused and names the field", func(t *testing.T) {
		for _, tc := range []struct {
			body  map[string]any
			field string
		}{
			{map[string]any{"fullName": "a"}, "fullName"},
			{map[string]any{"fullName": "   "}, "fullName"},
			{map[string]any{"nisn": "12a45"}, "nisn"},
			{map[string]any{"nisn": "1234"}, "nisn"},
			{map[string]any{"nisn": "1234567890123456789012345678901"}, "nisn"},
		} {
			res := patch(parent.Token, childID, tc.body)
			res.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
			require.Contains(t, res.Body["details"], tc.field)
		}

		patch(parent.Token, childID, map[string]any{}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		patch(parent.Token, "not-a-uuid", map[string]any{"fullName": "Anak Baru"}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("only the parent of the child can change it", func(t *testing.T) {
		patch(parent.Token, uuid.NewString(), map[string]any{"fullName": "Anak Baru"}).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		patch(other.Token, childID, map[string]any{"fullName": "Anak Baru"}).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		patch(guru.Token, childID, map[string]any{"fullName": "Anak Baru"}).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		patch("", childID, map[string]any{"fullName": "Anak Baru"}).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")

		require.Equal(t, "Zaki Terakhir", call(t, http.MethodGet, "/api/v1/wali/child/"+childID, parent.Token, nil).data("fullName"))
	})
}
