package mailretry_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kiboozapp/kibooz-backend/internal/app/auth/mailretry"
	"github.com/kiboozapp/kibooz-backend/internal/mailer"
)

type source struct {
	ids   []uuid.UUID
	err   error
	calls int
	limit int
	lease time.Duration
}

func (s *source) ClaimMailRetries(_ context.Context, _ time.Time, lease time.Duration, limit int) ([]uuid.UUID, error) {
	s.calls++
	s.limit, s.lease = limit, lease

	return s.ids, s.err
}

func ids(n int) []uuid.UUID {
	list := make([]uuid.UUID, n)
	for i := range list {
		list[i] = uuid.New()
	}

	return list
}

func TestRunMailsEveryAccountThatIsDue(t *testing.T) {
	src := &source{ids: ids(3)}

	var tried []uuid.UUID

	job := mailretry.NewJob(src, func(_ context.Context, id uuid.UUID) error {
		tried = append(tried, id)

		return nil
	})

	count, err := job.Run(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, count)
	require.Equal(t, src.ids, tried)
	require.Positive(t, src.limit)
	require.Greater(t, src.lease, time.Minute, "the lease is longer than the run")
}

func TestAFailureOfOneAccountDoesNotStopTheOthers(t *testing.T) {
	src := &source{ids: ids(3)}
	calls := 0

	job := mailretry.NewJob(src, func(context.Context, uuid.UUID) error {
		calls++
		if calls == 1 {
			return errors.New("database is down")
		}

		return nil
	})

	count, err := job.Run(context.Background())
	require.ErrorContains(t, err, "database is down")
	require.Equal(t, 3, count)
}

func TestAQuotaAnswerStopsTheRunAndThenTheJobForAWhile(t *testing.T) {
	src := &source{ids: ids(3)}
	calls := 0

	job := mailretry.NewJob(src, func(context.Context, uuid.UUID) error {
		calls++

		return fmt.Errorf("wrapped %w", mailer.ErrQuota)
	})

	count, err := job.Run(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, 1, calls, "the accounts after the refused one are left for later")

	// the next runs do not even look for accounts
	count, err = job.Run(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)
	require.Equal(t, 1, src.calls)
}

func TestARunReportsAFailedClaimAndStopsWithItsContext(t *testing.T) {
	_, err := mailretry.NewJob(&source{err: errors.New("database is down")}, nil).Run(context.Background())
	require.Error(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	count, err := mailretry.NewJob(&source{ids: ids(2)}, func(context.Context, uuid.UUID) error { return nil }).Run(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, count)
}

func TestStartRunsTheJobAndStopWaitsForIt(t *testing.T) {
	ran := make(chan struct{}, 1)
	observed := make(chan error, 1)

	job := mailretry.NewJob(&source{ids: ids(1)}, func(context.Context, uuid.UUID) error {
		select {
		case ran <- struct{}{}:
		default:
		}

		return nil
	}).Observe(func(err error) {
		select {
		case observed <- err:
		default:
		}
	})

	stop := job.Start(10 * time.Millisecond)

	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the job did not run")
	}

	require.NoError(t, <-observed)
	stop()

	mailretry.NewJob(&source{}, nil).Start(0)()
}

func TestAnAnswerAboutTheRateEndsTheRunAndNotTheJob(t *testing.T) {
	src := &source{ids: ids(3)}
	calls := 0

	job := mailretry.NewJob(src, func(context.Context, uuid.UUID) error {
		calls++

		return fmt.Errorf("wrapped %w", mailer.ErrRateLimit)
	})

	for range 2 {
		count, err := job.Run(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1, count)
	}

	require.Equal(t, 2, calls)
	require.Equal(t, 2, src.calls, "the next run looks again")
}
