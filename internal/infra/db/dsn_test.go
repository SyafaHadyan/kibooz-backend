package db

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/infra/env"
)

func TestBuildDSNKeepsAwkwardPasswordsIntact(t *testing.T) {
	passwords := []string{
		"plain-Password_123",
		"with space",
		`it's quoted`,
		`back\slash`,
		`both \' mixed`,
		"dollar$sign and #hash",
		"equals=sign&amp;percent%20",
		"unicode-kata-sandi-é",
		"",
	}

	for _, password := range passwords {
		cfg := &env.Env{
			DBHost: "db.internal", DBPort: 5432, DBUsername: "kibooz user",
			DBPassword: password, DBName: "kibooz", DBSSLMode: "require",
		}

		parsed, err := pgx.ParseConfig(buildDSN(cfg))
		require.NoError(t, err, "password %q", password)

		require.Equal(t, password, parsed.Password, "password %q", password)
		require.Equal(t, "kibooz user", parsed.User)
		require.Equal(t, "db.internal", parsed.Host)
		require.Equal(t, uint16(5432), parsed.Port)
		require.Equal(t, "kibooz", parsed.Database)
		require.Equal(t, "UTC", parsed.RuntimeParams["TimeZone"])
		require.Equal(t, "30000", parsed.RuntimeParams["idle_in_transaction_session_timeout"])
		require.Equal(t, 10*time.Second, parsed.ConnectTimeout, "a database that does not answer ends the connection after ten seconds")
	}
}
