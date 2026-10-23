package e2e

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func queuedWithPrefix(t *testing.T, prefix string) int {
	t.Helper()

	var count int

	require.NoError(t, testDB.Raw("SELECT count(*) FROM file_cleanups WHERE object_key LIKE ?", prefix+"%").Scan(&count).Error)

	return count
}

func TestATeacherRemovesAChildFromTheClass(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(tinyPNG)
	require.NoError(t, err)

	guru := registerGuru(t, "Hortensia")
	colleague := registerGuru(t, "Iris")
	outsider := registerGuru(t, "Jasmine")
	classID, code := classOf(t, guru)

	base := "/api/v1/guru/classes/" + classID
	require.Equal(t, http.StatusCreated, call(t, http.MethodPost, base+"/teachers", guru.Token, map[string]any{"email": colleague.Email}).Status)

	// the child with points ranks first, and the other two follow by name
	removedNISN := nisn()
	removedEmail := fmt.Sprintf("removed.%s@example.com", suffix())
	res := registerWaliWith(t, removedEmail, removedNISN, code, "Anak Ada")
	require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)

	removedParent := account{Token: res.data("token").(string), Email: removedEmail, Password: testPassword}
	removedID := call(t, http.MethodGet, "/api/v1/wali/dashboard", removedParent.Token, nil).data("student", "id").(string)
	second, secondID := registerWali(t, code, "Anak Beta")
	third, thirdID := registerWali(t, code, "Anak Cakra")

	avatar := upload(t, removedParent.Token, map[string]string{"studentId": removedID}, "foto.png", png)
	require.Equal(t, http.StatusOK, avatar.Status, "body %v", avatar.Body)

	avatarKey := bucketKey(t, avatar.data("avatarUrl").(string))

	// the sweep test counts every key in the queue, so what this test queues must not outlive it
	t.Cleanup(func() {
		_ = testDB.Exec("DELETE FROM file_cleanups WHERE object_key LIKE ? OR object_key LIKE ?", avatarKey+"%", "trash-scans/"+removedID+"/%").Error
	})

	claim := call(t, http.MethodPost, "/api/v1/trash/scan-claim", removedParent.Token, map[string]any{
		"studentId": removedID, "trashType": "B3", "confidenceScore": 0.8, "photoBase64": "data:image/png;base64," + tinyPNG,
	})
	require.Equal(t, http.StatusOK, claim.Status, "body %v", claim.Body)

	rank := func(token string, studentID string) any {
		return call(t, http.MethodGet, "/api/v1/wali/dashboard?studentId="+studentID, token, nil).data("pointsSummary", "classRank")
	}

	require.EqualValues(t, 1, rank(removedParent.Token, removedID))
	require.EqualValues(t, 2, rank(second.Token, secondID))
	require.EqualValues(t, 3, rank(third.Token, thirdID))

	remove := func(token string, classPath string, studentID string) result {
		return call(t, http.MethodDelete, "/api/v1/guru/classes/"+classPath+"/students/"+studentID, token, nil)
	}

	t.Run("people who may not remove a child are refused and nothing changes", func(t *testing.T) {
		remove(outsider.Token, classID, removedID).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		remove(removedParent.Token, classID, removedID).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		remove("", classID, removedID).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
		remove(guru.Token, classID, uuid.NewString()).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		remove(guru.Token, uuid.NewString(), removedID).requireError(t, http.StatusNotFound, "CLASS_NOT_FOUND")
		remove(guru.Token, "not-a-uuid", removedID).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		remove(guru.Token, classID, "not-a-uuid").requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		require.EqualValues(t, 1, rank(removedParent.Token, removedID))
		require.Zero(t, queuedWithPrefix(t, avatarKey))
	})

	t.Run("a child of another class is not found in this one", func(t *testing.T) {
		other := registerGuru(t, "Kenanga")
		otherClassID, otherCode := classOf(t, other)
		_, otherChild := registerWali(t, otherCode, "Anak Lain")

		remove(guru.Token, classID, otherChild).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		require.Equal(t, http.StatusOK, remove(other.Token, otherClassID, otherChild).Status)
	})

	t.Run("a teacher of the class removes the child, the others move up and the files are queued", func(t *testing.T) {
		res := remove(colleague.Token, classID, removedID)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		require.EqualValues(t, 1, rank(second.Token, secondID))
		require.EqualValues(t, 2, rank(third.Token, thirdID))

		roster := call(t, http.MethodGet, "/api/v1/classes/"+classID+"/students", guru.Token, nil)
		require.Equal(t, http.StatusOK, roster.Status, "body %v", roster.Body)
		require.EqualValues(t, 2, roster.data("total"))

		require.Equal(t, 1, queuedWithPrefix(t, avatarKey), "the avatar waits for the sweep")
		require.Equal(t, 1, queuedWithPrefix(t, "trash-scans/"+removedID+"/"), "so does the photo of the scan")

		require.Equal(t, http.StatusOK, call(t, http.MethodGet, "/api/v1/wali/dashboard", second.Token, nil).Status)
	})

	t.Run("the child is gone for the parent and cannot be removed twice", func(t *testing.T) {
		call(t, http.MethodGet, "/api/v1/wali/child/"+removedID, removedParent.Token, nil).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
		require.Empty(t, listOf(t, call(t, http.MethodGet, "/api/v1/wali/children", removedParent.Token, nil), "children"))

		remove(guru.Token, classID, removedID).requireError(t, http.StatusNotFound, "STUDENT_NOT_FOUND")
	})

	t.Run("the NISN of the removed child can be registered again in another class", func(t *testing.T) {
		other := registerGuru(t, "Lili")
		_, otherCode := classOf(t, other)

		res := call(t, http.MethodPost, "/api/v1/wali/children", removedParent.Token, map[string]any{
			"classCode": otherCode, "student": map[string]any{"nisn": removedNISN, "fullName": "Anak Ada"},
		})
		require.Equal(t, http.StatusCreated, res.Status, "body %v", res.Body)
	})
}
