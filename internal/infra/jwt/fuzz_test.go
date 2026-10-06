package jwt

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

func FuzzValidateToken(f *testing.F) {
	service := newService("0123456789abcdef0123456789abcdef", 60)

	valid, err := service.GenerateToken(uuid.New(), constants.RoleWali)
	require.NoError(f, err)

	f.Add(valid)
	f.Add(valid + "x")
	f.Add("")
	f.Add("a.b.c")
	f.Add("eyJhbGciOiJub25lIn0.eyJzdWIiOiIxIn0.")

	f.Fuzz(func(t *testing.T, token string) {
		claims, err := service.ValidateToken(token)
		if err != nil {
			require.Nil(t, claims)

			return
		}

		// only a token signed with the secret may pass, and it always carries a known role
		require.NotNil(t, claims)
		require.True(t, claims.Role.Valid())
	})
}
