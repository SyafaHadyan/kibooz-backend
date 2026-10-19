package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestATeacherClosesAClassForNewChildren(t *testing.T) {
	guru := registerGuru(t, "Edelweis")
	colleague := registerGuru(t, "Flamboyan")
	outsider := registerGuru(t, "Gardenia")
	classID, code := classOf(t, guru)
	parent, _ := registerWali(t, code, "Already In")

	base := "/api/v1/guru/classes/" + classID
	set := func(token string, body any) result {
		return call(t, http.MethodPut, base+"/joining", token, body)
	}

	added := call(t, http.MethodPost, base+"/teachers", guru.Token, map[string]any{"email": colleague.Email})
	require.Equal(t, http.StatusCreated, added.Status, "body %v", added.Body)

	t.Run("a new class starts open", func(t *testing.T) {
		require.Equal(t, true, call(t, http.MethodGet, base, guru.Token, nil).data("joinOpen"))
		require.Equal(t, true, call(t, http.MethodGet, "/api/v1/guru/dashboard?classId="+classID, guru.Token, nil).data("classOverview", "joinOpen"))

		created := call(t, http.MethodPost, "/api/v1/guru/classes", guru.Token, map[string]any{"name": "Open Class"})
		require.Equal(t, http.StatusCreated, created.Status, "body %v", created.Body)
		require.Equal(t, true, created.data("joinOpen"))
	})

	t.Run("a closed class refuses registration and adding a child, with the code unchanged", func(t *testing.T) {
		res := set(guru.Token, map[string]any{"joinOpen": false})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, false, res.data("joinOpen"))

		require.Equal(t, false, call(t, http.MethodGet, base, guru.Token, nil).data("joinOpen"))
		require.Equal(t, false, call(t, http.MethodGet, "/api/v1/guru/dashboard?classId="+classID, guru.Token, nil).data("classOverview", "joinOpen"))
		require.Equal(t, code, call(t, http.MethodGet, base, guru.Token, nil).data("joinCode"))

		registerWaliWith(t, fmt.Sprintf("closed.%s@example.com", suffix()), nisn(), code, "Closed Child").
			requireError(t, http.StatusConflict, "CLASS_JOINING_CLOSED")

		call(t, http.MethodPost, "/api/v1/wali/children", parent.Token, map[string]any{
			"classCode": code, "student": map[string]any{"nisn": nisn(), "fullName": "Second Child"},
		}).requireError(t, http.StatusConflict, "CLASS_JOINING_CLOSED")
	})

	t.Run("a wrong code is still not found and children already in the class are not affected", func(t *testing.T) {
		registerWaliWith(t, fmt.Sprintf("wrong.%s@example.com", suffix()), nisn(), "ZZZZZZZZ", "Wrong Child").
			requireError(t, http.StatusNotFound, "CLASS_CODE_NOT_FOUND")

		require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/api/v1/wali/dashboard", parent.Token, nil).Status)
	})

	t.Run("another teacher of the class opens it again", func(t *testing.T) {
		require.Equal(t, http.StatusOK, set(colleague.Token, map[string]any{"joinOpen": true}).Status)
		require.Equal(t, http.StatusOK, set(colleague.Token, map[string]any{"joinOpen": true}).Status, "the same value again changes nothing")

		res := registerWaliWith(t, fmt.Sprintf("open.%s@example.com", suffix()), nisn(), code, "Open Child")
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
	})

	t.Run("only a teacher of the class can switch it and the body is checked", func(t *testing.T) {
		set(outsider.Token, map[string]any{"joinOpen": false}).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		set(parent.Token, map[string]any{"joinOpen": false}).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		set("", map[string]any{"joinOpen": false}).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		set(guru.Token, map[string]any{}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		set(guru.Token, map[string]any{"joinOpen": "no"}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPut, "/api/v1/guru/classes/"+uuid.NewString()+"/joining", guru.Token, map[string]any{"joinOpen": false}).
			requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		call(t, http.MethodPut, "/api/v1/guru/classes/not-a-uuid/joining", guru.Token, map[string]any{"joinOpen": false}).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		require.Equal(t, true, call(t, http.MethodGet, base, guru.Token, nil).data("joinOpen"), "the refused calls changed nothing")
	})
}
