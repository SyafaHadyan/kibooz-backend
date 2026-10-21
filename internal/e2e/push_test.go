package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const pushBase = "/api/v1/push/tokens"

func pushToken(label string) string {
	return "fcm-" + label + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func tokenCount(t *testing.T, where string, args ...any) int64 {
	t.Helper()

	var count int64

	require.NoError(t, testDB.Raw("SELECT COUNT(*) FROM push_tokens WHERE "+where, args...).Scan(&count).Error)

	return count
}

func TestPushTokensBelongToOneAccountAtATime(t *testing.T) {
	guru := registerGuru(t, "Pushbunga")
	_, code := classOf(t, guru)
	parent, _ := registerWali(t, code, "Push Child")

	token := pushToken("a")

	t.Run("a token is saved once however often it is sent", func(t *testing.T) {
		for range 2 {
			res := call(t, http.MethodPost, pushBase, parent.Token, map[string]any{"token": token})
			require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		}

		require.EqualValues(t, 1, tokenCount(t, "token = ?", token))

		var platform string

		require.NoError(t, testDB.Raw("SELECT platform FROM push_tokens WHERE token = ?", token).Scan(&platform).Error)
		require.Equal(t, "android", platform, "android is the default")
	})

	t.Run("a phone that signs in as somebody else moves its token", func(t *testing.T) {
		res := call(t, http.MethodPost, pushBase, guru.Token, map[string]any{"token": token, "platform": "android"})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		require.EqualValues(t, 1, tokenCount(t, "token = ?", token), "still one row")

		var owner string

		require.NoError(t, testDB.Raw("SELECT user_id::text FROM push_tokens WHERE token = ?", token).Scan(&owner).Error)

		contact := chatContactOf(t, call(t, http.MethodGet, chatBase+"/contacts", parent.Token, nil), "")
		require.Equal(t, contact["id"], owner, "the token now belongs to the teacher")
	})

	t.Run("removing a token needs the account that holds it", func(t *testing.T) {
		res := call(t, http.MethodDelete, pushBase, parent.Token, map[string]any{"token": token})
		require.Equal(t, http.StatusOK, res.Status, "a token of somebody else is left alone without an error")
		require.EqualValues(t, 1, tokenCount(t, "token = ?", token))

		res = call(t, http.MethodDelete, pushBase, guru.Token, map[string]any{"token": token})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.EqualValues(t, 0, tokenCount(t, "token = ?", token))

		again := call(t, http.MethodDelete, pushBase, guru.Token, map[string]any{"token": token})
		require.Equal(t, http.StatusOK, again.Status, "removing twice is safe")
	})

	t.Run("a bad token or a missing sign in is refused", func(t *testing.T) {
		call(t, http.MethodPost, pushBase, parent.Token, map[string]any{}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, pushBase, parent.Token, map[string]any{"token": "short"}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, pushBase, parent.Token, map[string]any{"token": strings.Repeat("a", 4097)}).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, pushBase, parent.Token, map[string]any{"token": strings.Repeat("a", 20) + " " + strings.Repeat("b", 20)}).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, pushBase, parent.Token, map[string]any{"token": pushToken("ios"), "platform": "ios"}).
			requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodDelete, pushBase, parent.Token, map[string]any{}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		call(t, http.MethodPost, pushBase, "", map[string]any{"token": token}).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
	})

	t.Run("an account keeps its ten newest tokens", func(t *testing.T) {
		var first string

		for i := range 12 {
			value := pushToken("many")
			if i == 0 {
				first = value
			}

			res := call(t, http.MethodPost, pushBase, parent.Token, map[string]any{"token": value})
			require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		}

		var total int64

		require.NoError(t, testDB.Raw("SELECT COUNT(*) FROM push_tokens AS p JOIN users AS u ON u.id = p.user_id WHERE u.email = ?", parent.Email).Scan(&total).Error)
		require.EqualValues(t, 10, total)
		require.EqualValues(t, 0, tokenCount(t, "token = ?", first), "the oldest one went")
	})

	t.Run("deleting the account removes its tokens", func(t *testing.T) {
		res := call(t, http.MethodDelete, "/api/v1/users/me", parent.Token, map[string]any{"password": testPassword})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)

		var total int64

		require.NoError(t, testDB.Raw("SELECT COUNT(*) FROM push_tokens AS p JOIN users AS u ON u.id = p.user_id WHERE u.email = ?", parent.Email).Scan(&total).Error)
		require.EqualValues(t, 0, total)

		// the access token still works until it expires, and it must not bring a token back for a deleted account
		late := call(t, http.MethodPost, pushBase, parent.Token, map[string]any{"token": strings.Repeat("z", 30)})
		late.requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_INVALID")

		require.NoError(t, testDB.Raw("SELECT COUNT(*) FROM push_tokens AS p JOIN users AS u ON u.id = p.user_id WHERE u.email = ?", parent.Email).Scan(&total).Error)
		require.EqualValues(t, 0, total)
	})
}
