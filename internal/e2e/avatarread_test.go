package e2e

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/app/user/repository"
)

// The avatar is read again after an update that failed, to learn whether it was committed after all. The read has to find the address of
// the account or of the child of the parent, and nothing for anybody else.
func TestTheAvatarReadFindsOnlyWhatBelongsToTheCaller(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(tinyPNG)
	require.NoError(t, err)

	guru := registerGuru(t, "Avatar Dibaca")
	classID, code := classOf(t, guru)
	parent, childID := registerWali(t, code, "Anak Avatar")
	stranger, _ := registerWali(t, code, "Anak Lain")

	var userID string

	require.NoError(t, testDB.Raw("SELECT id::text FROM users WHERE email = ?", guru.Email).Scan(&userID).Error)

	repo := repository.NewUserDB(testDB)
	ctx := context.Background()

	t.Run("an account without a picture has an empty address", func(t *testing.T) {
		url, found, err := repo.UserAvatar(ctx, uuid.MustParse(userID))
		require.NoError(t, err)
		require.True(t, found)
		require.Empty(t, url)

		_, found, err = repo.UserAvatar(ctx, uuid.New())
		require.NoError(t, err)
		require.False(t, found)
	})

	t.Run("the address of the account is read after an upload", func(t *testing.T) {
		res := upload(t, guru.Token, nil, "foto.png", png)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		url, found, err := repo.UserAvatar(ctx, uuid.MustParse(userID))
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, res.data("avatarUrl"), url)
	})

	t.Run("the address and the class of a child are read for its own parent only", func(t *testing.T) {
		res := upload(t, parent.Token, map[string]string{"studentId": childID}, "foto.png", png)
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		waliID := uuid.MustParse(waliIDOfEmail(t, parent.Email))

		url, class, found, err := repo.StudentAvatar(ctx, waliID, uuid.MustParse(childID))
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, res.data("avatarUrl"), url)
		require.Equal(t, classID, class.String())

		_, _, found, err = repo.StudentAvatar(ctx, uuid.MustParse(waliIDOfEmail(t, stranger.Email)), uuid.MustParse(childID))
		require.NoError(t, err)
		require.False(t, found, "the child of another parent is not found")
	})
}
