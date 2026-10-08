package classcode

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestNewCodeUsesTheAlphabetAndTheFixedLength(t *testing.T) {
	seen := map[string]bool{}

	for range 200 {
		code, err := newCode()

		require.NoError(t, err)
		require.Len(t, code, length)

		for _, char := range code {
			require.True(t, strings.ContainsRune(alphabet, char), "unexpected %q in %s", char, code)
		}

		seen[code] = true
	}

	require.Greater(t, len(seen), 150, "codes must be random")
}

func TestTheAlphabetLeavesOutConfusingCharacters(t *testing.T) {
	for _, char := range "01IO" {
		require.False(t, strings.ContainsRune(alphabet, char), "%q is easy to confuse", char)
	}
}

func TestOnlyATakenCodeIsRetried(t *testing.T) {
	taken := &pgconn.PgError{Code: uniqueViolation, ConstraintName: codeConstraint}

	require.True(t, isCodeTaken(taken))
	require.True(t, isCodeTaken(fmt.Errorf("wrapped %w", taken)))
	require.False(t, isCodeTaken(&pgconn.PgError{Code: uniqueViolation, ConstraintName: "users_email_key"}))
	require.False(t, isCodeTaken(&pgconn.PgError{Code: "23503", ConstraintName: codeConstraint}))
	require.False(t, isCodeTaken(errors.New("connection lost")))
	require.False(t, isCodeTaken(nil))
}
