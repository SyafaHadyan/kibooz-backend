package apperror

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInternalMapsADeadlineOrACancelledRequestToAServerTimeout(t *testing.T) {
	for name, cause := range map[string]error{
		"a deadline":             context.DeadlineExceeded,
		"a wrapped deadline":     fmt.Errorf("begin: %w", context.DeadlineExceeded),
		"a cancelled request":    context.Canceled,
		"a wrapped cancellation": fmt.Errorf("query: %w", context.Canceled),
	} {
		t.Run(name, func(t *testing.T) {
			got := Internal(cause)

			require.Equal(t, http.StatusServiceUnavailable, got.Status)
			require.Equal(t, "SERVER_TIMEOUT", got.Code)
			require.Equal(t, 1, got.RetryAfterSeconds)
			require.ErrorIs(t, got, cause, "the cause is kept for the log")
		})
	}
}

func TestInternalKeepsEveryOtherFailureAs500(t *testing.T) {
	got := Internal(errors.New("a bug"))

	require.Equal(t, http.StatusInternalServerError, got.Status)
	require.Equal(t, "INTERNAL_ERROR", got.Code)
	require.Zero(t, got.RetryAfterSeconds)
}

func TestAsMapsARawDeadlineToo(t *testing.T) {
	require.Equal(t, "SERVER_TIMEOUT", As(context.DeadlineExceeded).Code)
	require.Same(t, ErrBusy, As(ErrBusy))
}

func TestTheSharedServerTimeoutErrorIsNeverChanged(t *testing.T) {
	Internal(context.DeadlineExceeded)

	require.Nil(t, ErrServerTimeout.Err, "WithErr works on a copy")
}
