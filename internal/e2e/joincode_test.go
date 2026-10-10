package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRotateJoinCode(t *testing.T) {
	guru := registerGuru(t, "Rotate")
	classID, oldCode := classOf(t, guru)

	otherGuru := registerGuru(t, "Not Mine")
	otherClassID, otherCode := classOf(t, otherGuru)

	joined, _ := registerWali(t, oldCode, "Joined Before")

	path := "/api/v1/guru/classes/" + classID + "/join-code"

	var newCode string

	t.Run("a teacher gets a new code and the old one stops working", func(t *testing.T) {
		res := call(t, http.MethodPost, path, guru.Token, nil)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		newCode = res.data("joinCode").(string)
		require.Len(t, newCode, 6)
		require.NotEqual(t, oldCode, newCode)

		dashboard := call(t, http.MethodGet, "/api/v1/guru/dashboard?classId="+classID, guru.Token, nil)
		require.Equal(t, newCode, dashboard.data("classOverview", "joinCode"))

		list := call(t, http.MethodGet, "/api/v1/guru/classes/"+classID, guru.Token, nil)
		require.Equal(t, newCode, list.data("joinCode"))

		registerWaliWith(t, fmt.Sprintf("late.%s@example.com", suffix()), nisn(), oldCode, "Late Child").
			requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
	})

	t.Run("the new code lets a parent join", func(t *testing.T) {
		res := registerWaliWith(t, fmt.Sprintf("new.%s@example.com", suffix()), nisn(), newCode, "New Child")
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
	})

	t.Run("a parent who joined before is not affected", func(t *testing.T) {
		dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard", joined.Token, nil)
		require.Equal(t, http.StatusOK, dashboard.Status, "body %v", dashboard.Body)
	})

	t.Run("every rotation makes another code", func(t *testing.T) {
		second := call(t, http.MethodPost, path, guru.Token, nil).data("joinCode").(string)
		require.NotEqual(t, newCode, second)

		registerWaliWith(t, fmt.Sprintf("stale.%s@example.com", suffix()), nisn(), newCode, "Stale Child").
			requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
	})

	t.Run("only a teacher of the class can replace the code", func(t *testing.T) {
		call(t, http.MethodPost, path, otherGuru.Token, nil).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		call(t, http.MethodPost, path, joined.Token, nil).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		call(t, http.MethodPost, path, "", nil).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		call(t, http.MethodPost, "/api/v1/guru/classes/"+uuid.NewString()+"/join-code", guru.Token, nil).
			requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		call(t, http.MethodPost, "/api/v1/guru/classes/not-a-uuid/join-code", guru.Token, nil).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
	})

	t.Run("the code of another class is not touched", func(t *testing.T) {
		dashboard := call(t, http.MethodGet, "/api/v1/guru/dashboard?classId="+otherClassID, otherGuru.Token, nil)
		require.Equal(t, otherCode, dashboard.data("classOverview", "joinCode"))
	})
}
