package repository

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestOnlyAForeignKeyViolationIsRecognized(t *testing.T) {
	require.True(t, isForeignKeyViolation(&pgconn.PgError{Code: "23503"}))
	require.True(t, isForeignKeyViolation(fmt.Errorf("insert: %w", &pgconn.PgError{Code: "23503"})), "wrapped by the driver or gorm")

	require.False(t, isForeignKeyViolation(&pgconn.PgError{Code: "23505"}), "a unique violation is another error")
	require.False(t, isForeignKeyViolation(errors.New("connection reset")))
	require.False(t, isForeignKeyViolation(nil))
}
