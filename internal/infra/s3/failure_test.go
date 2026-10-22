package s3

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
)

func TestAStorageCallThatRanOutOfTimeIsAServerTimeoutAndNotABadGateway(t *testing.T) {
	for name, cause := range map[string]error{
		"a deadline":       fmt.Errorf("operation error S3: PutObject, %w", context.DeadlineExceeded),
		"a cancelled call": fmt.Errorf("operation error S3: HeadObject, %w", context.Canceled),
	} {
		t.Run(name, func(t *testing.T) {
			var appErr *apperror.Error

			require.ErrorAs(t, storageFailure(cause), &appErr)
			require.Equal(t, http.StatusServiceUnavailable, appErr.Status)
			require.Equal(t, "SERVER_TIMEOUT", appErr.Code)
		})
	}
}

func TestAnyOtherStorageFailureIsABadGateway(t *testing.T) {
	var appErr *apperror.Error

	cause := errors.New("403 AccessDenied")

	require.ErrorAs(t, storageFailure(cause), &appErr)
	require.Equal(t, http.StatusBadGateway, appErr.Status)
	require.Equal(t, "STORAGE_ERROR", appErr.Code)
	require.ErrorIs(t, appErr, cause)
}
