package sessions

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAZeroIntervalSwitchesTheSweepOff(t *testing.T) {
	stop := NewSweeper(nil, time.Hour).Start(0)

	require.NotPanics(t, stop, "there is nothing running to wait for")
}

// stub makes the sweeper answer from a script, one answer for each batch
func stub(answers ...func() (int64, error)) *Sweeper {
	sweeper := NewSweeper(nil, time.Hour)

	var calls atomic.Int32

	sweeper.batch = func(context.Context) (int64, error) {
		index := int(calls.Add(1)) - 1
		if index >= len(answers) {
			return 0, nil
		}

		return answers[index]()
	}

	return sweeper
}

func rows(n int64) func() (int64, error) { return func() (int64, error) { return n, nil } }

func TestASweepKeepsGoingWhileTheBatchesAreFull(t *testing.T) {
	removed, err := stub(rows(sweepBatch), rows(sweepBatch), rows(7)).Sweep(context.Background())

	require.NoError(t, err)
	require.Equal(t, 2*sweepBatch+7, removed)
}

func TestASweepStopsAtTheFirstFailureAndSaysHowFarItGot(t *testing.T) {
	boom := errors.New("boom")

	removed, err := stub(rows(sweepBatch), func() (int64, error) { return 0, boom }).Sweep(context.Background())

	require.ErrorIs(t, err, boom)
	require.Equal(t, sweepBatch, removed)
}

func TestASweepStopsWhenItsContextEndsBetweenFullBatches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	removed, err := stub(func() (int64, error) {
		cancel()

		return sweepBatch, nil
	}, rows(sweepBatch)).Sweep(ctx)

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, sweepBatch, removed)
}

func TestTheSweepRunsOnEveryTickAndReportsEachResult(t *testing.T) {
	results := make(chan error, 8)

	sweeper := stub(rows(3), func() (int64, error) { return 0, errors.New("boom") }).Observe(func(err error) { results <- err })
	stop := sweeper.Start(5 * time.Millisecond)

	first := <-results
	second := <-results

	stop()

	require.NoError(t, first)
	require.ErrorContains(t, second, "boom")
}

func TestTheFirstSweepComesSoonAfterTheStartAndNotAfterAWholeInterval(t *testing.T) {
	results := make(chan error, 2)

	sweeper := stub(rows(1)).Observe(func(err error) { results <- err })
	sweeper.startAfter = 5 * time.Millisecond

	stop := sweeper.Start(time.Hour)
	defer stop()

	select {
	case err := <-results:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "the first sweep waited for the whole interval")
	}
}
