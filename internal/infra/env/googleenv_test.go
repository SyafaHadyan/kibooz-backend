package env

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGoogleClientIDsParsesCommaSeparatedValues(t *testing.T) {
	t.Setenv("DB_NAME", "kibooz")
	t.Setenv("DB_USERNAME", "kibooz")
	t.Setenv("DB_PASSWORD", strings.Repeat("d", 12))
	t.Setenv("JWT_SECRET_KEY", strings.Repeat("s", 32))
	t.Setenv("GOOGLE_CLIENT_IDS", "client-a.example.test,client-b.example.test")

	cfg, err := New()
	require.NoError(t, err)
	require.Equal(t, []string{"client-a.example.test", "client-b.example.test"}, cfg.GoogleClientIDs)
}
