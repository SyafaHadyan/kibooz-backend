package e2e

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAParentDeletesTheirLastChild(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(tinyPNG)
	require.NoError(t, err)

	guru := registerGuru(t, "Magnolia Kecil")
	classID, code := classOf(t, guru)

	childNISN := nisn()
	email := fmt.Sprintf("last.%s@example.com", suffix())
	res := registerWaliWith(t, email, childNISN, code, "Anak Ada")
	require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

	parent := account{Token: res.data("token").(string), Email: email, Password: testPassword}
	childID := call(t, http.MethodGet, "/api/v1/wali/dashboard", parent.Token, nil).data("student", "id").(string)
	other, otherID := registerWali(t, code, "Anak Beta")

	avatar := upload(t, parent.Token, map[string]string{"studentId": childID}, "foto.png", png)
	require.Equal(t, http.StatusOK, avatar.Status, "body %v", avatar.Body)

	avatarKey := bucketKey(t, avatar.data("avatarUrl").(string))

	// the sweep test counts every key in the queue, so what this test queues must not outlive it
	t.Cleanup(func() {
		_ = testDB.Exec("DELETE FROM file_cleanups WHERE object_key LIKE ? OR object_key LIKE ?", avatarKey+"%", "trash-scans/"+childID+"/%").Error
	})

	claim := call(t, http.MethodPost, "/api/v1/trash/scan-claim", parent.Token, map[string]any{
		"studentId": childID, "trashType": "B3", "confidenceScore": 0.8, "photoBase64": "data:image/png;base64," + tinyPNG,
	})
	require.Equal(t, http.StatusOK, claim.Status, "body %v", claim.Body)

	rank := func(token string, id string) any {
		return call(t, http.MethodGet, "/api/v1/wali/dashboard?studentId="+id, token, nil).data("pointsSummary", "classRank")
	}

	require.EqualValues(t, 1, rank(parent.Token, childID))
	require.EqualValues(t, 2, rank(other.Token, otherID))

	remove := func(token string, id string) result {
		return call(t, http.MethodDelete, "/api/v1/wali/children/"+id, token, nil)
	}

	t.Run("only the parent of the child can delete it", func(t *testing.T) {
		remove(other.Token, childID).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		remove(guru.Token, childID).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		remove("", childID).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		remove(parent.Token, uuid.NewString()).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		remove(parent.Token, "not-a-uuid").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		require.EqualValues(t, 1, rank(parent.Token, childID))
		require.Zero(t, queuedWithPrefix(t, avatarKey))
	})

	t.Run("deleting the last child hides it, renumbers the class and queues the files", func(t *testing.T) {
		res := remove(parent.Token, childID)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, "Child deleted", res.Body["message"])

		require.EqualValues(t, 1, rank(other.Token, otherID))

		roster := call(t, http.MethodGet, "/api/v1/classes/"+classID+"/students", guru.Token, nil)
		require.Equal(t, http.StatusOK, roster.Status, "body %v", roster.Body)
		require.EqualValues(t, 1, roster.data("total"))

		require.Equal(t, 1, queuedWithPrefix(t, avatarKey), "the avatar waits for the sweep")
		require.Equal(t, 1, queuedWithPrefix(t, "trash-scans/"+childID+"/"), "so does the photo of the scan")

		remove(parent.Token, childID).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
	})

	t.Run("a parent with no child gets a clear answer", func(t *testing.T) {
		require.Empty(t, listOf(t, call(t, http.MethodGet, "/api/v1/wali/children", parent.Token, nil), "children"))

		call(t, http.MethodGet, "/api/v1/wali/dashboard", parent.Token, nil).requireError(t, http.StatusNotFound, "NO_CHILD_REGISTERED")
		call(t, http.MethodGet, "/api/v1/trash/stats", parent.Token, nil).requireError(t, http.StatusNotFound, "NO_CHILD_REGISTERED")
		call(t, http.MethodGet, "/api/v1/wali/dashboard?studentId="+childID, parent.Token, nil).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		call(t, http.MethodGet, "/api/v1/wali/child/"+childID, parent.Token, nil).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
	})

	t.Run("the parent adds a child again with the NISN that was freed", func(t *testing.T) {
		res := call(t, http.MethodPost, "/api/v1/wali/children", parent.Token, map[string]any{
			"classCode": code, "student": map[string]any{"nisn": childNISN, "fullName": "Anak Ada Lagi"},
		})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

		dashboard := call(t, http.MethodGet, "/api/v1/wali/dashboard", parent.Token, nil)
		require.Equal(t, http.StatusOK, dashboard.Status, "body %v", dashboard.Body)
		require.Equal(t, "Anak Ada Lagi", dashboard.data("student", "fullName"))
	})
}
