package pending_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kiboozapp/kibooz-backend/internal/app/auth/pending"
	"github.com/kiboozapp/kibooz-backend/internal/app/auth/repository"
	"github.com/kiboozapp/kibooz-backend/internal/constants"
)

type source struct {
	accounts []repository.PendingAccount
	err      error
	before   time.Time
	limit    int
}

func (s *source) OlderPendingAccounts(_ context.Context, before time.Time, limit int) ([]repository.PendingAccount, error) {
	s.before, s.limit = before, limit

	return s.accounts, s.err
}

func accounts(n int) []repository.PendingAccount {
	list := make([]repository.PendingAccount, n)
	for i := range list {
		list[i] = repository.PendingAccount{ID: uuid.New(), Role: constants.RoleWali}
	}

	return list
}

func TestSweepRemovesTheAccountsThatWaitedAWeek(t *testing.T) {
	src := &source{accounts: accounts(3)}

	var removed []uuid.UUID

	sweeper := pending.NewSweeper(src, func(_ context.Context, a repository.PendingAccount, _ time.Time) error {
		removed = append(removed, a.ID)

		return nil
	})

	before := time.Now()
	count, err := sweeper.Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, count)
	require.Len(t, removed, 3)
	require.WithinDuration(t, before.Add(-constants.PendingPurgeAfterDays*24*time.Hour), src.before, time.Minute)
	require.Positive(t, src.limit)
}

func TestSweepSkipsAnAccountThatCannotBeRemoved(t *testing.T) {
	list := accounts(3)
	sweeper := pending.NewSweeper(&source{accounts: list}, func(_ context.Context, a repository.PendingAccount, _ time.Time) error {
		if a.ID == list[1].ID {
			return errors.New("refused")
		}

		return nil
	})

	count, err := sweeper.Sweep(context.Background())
	require.ErrorContains(t, err, list[1].ID.String())
	require.Equal(t, 2, count)
}

func TestSweepReportsAFailedLookup(t *testing.T) {
	sweeper := pending.NewSweeper(&source{err: errors.New("database is down")}, func(context.Context, repository.PendingAccount, time.Time) error { return nil })

	_, err := sweeper.Sweep(context.Background())
	require.Error(t, err)
}

func TestSweepStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sweeper := pending.NewSweeper(&source{accounts: accounts(2)}, func(context.Context, repository.PendingAccount, time.Time) error { return nil })

	count, err := sweeper.Sweep(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, count)
}

func TestStartRunsTheSweepAndStopWaitsForIt(t *testing.T) {
	ran := make(chan struct{}, 1)
	observed := make(chan error, 1)

	sweeper := pending.NewSweeper(&source{accounts: accounts(1)}, func(context.Context, repository.PendingAccount, time.Time) error {
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

	// the first run happens a minute after the start, so a short interval makes it sooner
	stop := sweeper.Start(10 * time.Millisecond)

	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the sweep did not run")
	}

	require.NoError(t, <-observed)
	stop()

	// a zero interval switches it off
	pending.NewSweeper(&source{}, nil).Start(0)()
}
