package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAParentEditsTheirProfile(t *testing.T) {
	guru := registerGuru(t, "Kemboja")
	_, code := classOf(t, guru)
	parent, studentID := registerWali(t, code, "Anak Profil")
	other, _ := registerWali(t, code, "Anak Lain")

	patch := func(token string, body any) result {
		return call(t, http.MethodPatch, "/api/v1/wali/profile", token, body)
	}

	guardian := func(token string) result {
		return call(t, http.MethodGet, "/api/v1/wali/child/"+studentID, token, nil)
	}

	t.Run("a parent changes every field and the child profile shows it", func(t *testing.T) {
		res := patch(parent.Token, map[string]any{
			"fullName": "  Sarah Kartika Baru  ", "phoneNumber": " +62 812-3456-7890 ", "whatsappNumber": "0812 3456 7891", "address": " Jl. Mawar 3 ",
		})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, "Profile updated", res.Body["message"])
		require.Equal(t, "Sarah Kartika Baru", res.data("fullName"))
		require.Equal(t, parent.Email, res.data("email"))
		require.Equal(t, "+62 812-3456-7890", res.data("phoneNumber"))
		require.Equal(t, "0812 3456 7891", res.data("whatsappNumber"))
		require.Equal(t, "Jl. Mawar 3", res.data("address"))

		profile := guardian(parent.Token)
		require.Equal(t, "Sarah Kartika Baru", profile.data("guardian", "fullName"))
		require.Equal(t, "+62 812-3456-7890", profile.data("guardian", "phoneNumber"))
		require.Equal(t, "Jl. Mawar 3", profile.data("guardian", "address"))
	})

	t.Run("a missing field stays and an empty text clears", func(t *testing.T) {
		onlyAddress := patch(parent.Token, map[string]any{"address": "Jl. Veteran 2"})
		require.Equal(t, http.StatusOK, onlyAddress.Status, "body %v", onlyAddress.Body)
		require.Equal(t, "Sarah Kartika Baru", onlyAddress.data("fullName"))
		require.Equal(t, "0812 3456 7891", onlyAddress.data("whatsappNumber"))
		require.Equal(t, "Jl. Veteran 2", onlyAddress.data("address"))

		cleared := patch(parent.Token, map[string]any{"whatsappNumber": "", "phoneNumber": "  "})
		require.Equal(t, http.StatusOK, cleared.Status, "body %v", cleared.Body)
		require.Nil(t, cleared.data("whatsappNumber"))
		require.Nil(t, cleared.data("phoneNumber"))
		require.Equal(t, "Jl. Veteran 2", cleared.data("address"))
	})

	t.Run("invalid changes are refused and name the field", func(t *testing.T) {
		for _, tc := range []struct {
			body  map[string]any
			field string
		}{
			{map[string]any{"fullName": "   "}, "fullName"},
			{map[string]any{"fullName": strings.Repeat("a", 151)}, "fullName"},
			{map[string]any{"phoneNumber": "abc"}, "phoneNumber"},
			{map[string]any{"phoneNumber": strings.Repeat("1", 31)}, "phoneNumber"},
			{map[string]any{"whatsappNumber": "12345"}, "whatsappNumber"},
			{map[string]any{"address": strings.Repeat("a", 1001)}, "address"},
		} {
			res := patch(parent.Token, tc.body)
			res.requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
			require.Contains(t, res.Body["details"], tc.field)
		}

		patch(parent.Token, map[string]any{}).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")
		patch(parent.Token, nil).requireError(t, http.StatusBadRequest, "VALIDATION_ERROR")

		require.Equal(t, "Sarah Kartika Baru", guardian(parent.Token).data("guardian", "fullName"), "the refused calls changed nothing")
	})

	t.Run("the email cannot be changed and other parents are not touched", func(t *testing.T) {
		res := patch(parent.Token, map[string]any{"email": "new@example.com", "address": "Jl. Baru 4"})
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
		require.Equal(t, parent.Email, res.data("email"), "an unknown field is ignored")

		otherProfile := call(t, http.MethodGet, "/api/v1/wali/children", other.Token, nil)
		require.Equal(t, http.StatusOK, otherProfile.Status)
	})

	t.Run("only a parent uses the parent profile", func(t *testing.T) {
		patch(guru.Token, map[string]any{"fullName": "x"}).requireError(t, http.StatusForbidden, "AUTH_FORBIDDEN")
		patch("", map[string]any{"fullName": "x"}).requireError(t, http.StatusUnauthorized, "AUTH_TOKEN_MISSING")
	})
}
