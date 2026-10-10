package e2e

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// bucketKey turns the public address of an uploaded file back into the key of its object
func bucketKey(t *testing.T, address string) string {
	t.Helper()

	key, found := strings.CutPrefix(address, testPublicURL+"/")
	require.True(t, found, address)

	return key
}

func TestAccountDeletionRemovesItsFiles(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(tinyPNG)
	require.NoError(t, err)

	guru := registerGuru(t, "Files")
	_, joinCode := classOf(t, guru)
	wali, studentID := registerWali(t, joinCode, "Child Of Files")
	other, otherStudentID := registerWali(t, joinCode, "Child Of Someone Else")

	avatar := func(token string, fields map[string]string) string {
		res := upload(t, token, fields, "foto.png", png)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		return bucketKey(t, res.data("avatarUrl").(string))
	}

	claimWithPhoto := func(token string, childID string) {
		res := call(t, http.MethodPost, "/api/v1/trash/scan-claim", token, map[string]any{
			"studentId": childID, "trashType": "B3", "confidenceScore": 0.8,
			"photoBase64": "data:image/png;base64," + tinyPNG,
		})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
	}

	guruAvatar := avatar(guru.Token, nil)
	parentAvatar := avatar(wali.Token, nil)
	childAvatar := avatar(wali.Token, map[string]string{"studentId": studentID})
	otherChildAvatar := avatar(other.Token, map[string]string{"studentId": otherStudentID})

	claimWithPhoto(wali.Token, studentID)
	claimWithPhoto(wali.Token, studentID)
	claimWithPhoto(other.Token, otherStudentID)

	for _, key := range []string{guruAvatar, parentAvatar, childAvatar, otherChildAvatar} {
		require.Len(t, uploaded(key), 1, key)
	}

	require.Len(t, uploaded("trash-scans/"+studentID+"/"), 2)
	require.Len(t, uploaded("trash-scans/"+otherStudentID+"/"), 1)

	t.Run("a parent takes their own avatar, the avatar of the child and the photos of the child with them", func(t *testing.T) {
		require.Equal(t, http.StatusOK, deleteAccount(t, wali.Token, wali.Password).Status)

		require.Empty(t, uploaded(parentAvatar))
		require.Empty(t, uploaded(childAvatar))
		require.Empty(t, uploaded("trash-scans/"+studentID+"/"))
	})

	t.Run("the files of other accounts stay", func(t *testing.T) {
		require.Len(t, uploaded(guruAvatar), 1)
		require.Len(t, uploaded(otherChildAvatar), 1)
		require.Len(t, uploaded("trash-scans/"+otherStudentID+"/"), 1)
	})

	t.Run("a teacher takes their avatar with them", func(t *testing.T) {
		require.Equal(t, http.StatusOK, deleteAccount(t, guru.Token, guru.Password).Status)

		require.Empty(t, uploaded(guruAvatar))
		require.Len(t, uploaded(otherChildAvatar), 1)
	})
}
