package ranking

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetryRunsAgainWhileAChildMoved(t *testing.T) {
	calls := 0

	err := Retry(func() error {
		calls++
		if calls < 3 {
			return fmt.Errorf("transaction ended, %w", ErrMoved)
		}

		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, calls)
}

func TestRetryGivesUpAfterThreeTries(t *testing.T) {
	calls := 0

	err := Retry(func() error {
		calls++

		return ErrMoved
	})
	require.ErrorIs(t, err, ErrMoved)
	require.Equal(t, 3, calls)
}

func TestRetryDoesNotRepeatAnyOtherError(t *testing.T) {
	calls := 0
	failure := errors.New("connection reset")

	err := Retry(func() error {
		calls++

		return failure
	})
	require.ErrorIs(t, err, failure)
	require.Equal(t, 1, calls)
}
