package keepalive_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/keepalive"
)

const tick = 5 * time.Millisecond

func counting(calls *atomic.Int64, err error) keepalive.Target {
	return keepalive.Target{
		Name: "dependency",
		Ping: func(context.Context) error {
			calls.Add(1)

			return err
		},
	}
}

func TestStartPingsEveryTarget(t *testing.T) {
	var database, cache atomic.Int64

	stop := keepalive.Start(tick, counting(&database, nil), counting(&cache, nil))
	defer stop()

	require.Eventually(t, func() bool { return database.Load() >= 3 && cache.Load() >= 3 }, 2*time.Second, tick)
}

func TestFailingTargetDoesNotStopTheOthers(t *testing.T) {
	var broken, healthy atomic.Int64

	stop := keepalive.Start(tick, counting(&broken, errors.New("down")), counting(&healthy, nil))
	defer stop()

	require.Eventually(t, func() bool { return broken.Load() >= 3 && healthy.Load() >= 3 }, 2*time.Second, tick)
}

func TestStopEndsThePings(t *testing.T) {
	var calls atomic.Int64

	stop := keepalive.Start(tick, counting(&calls, nil))

	require.Eventually(t, func() bool { return calls.Load() >= 1 }, 2*time.Second, tick)

	stop()

	after := calls.Load()

	time.Sleep(10 * tick)
	require.Equal(t, after, calls.Load())
}

func TestDisabledIntervalNeverPings(t *testing.T) {
	var calls atomic.Int64

	for _, interval := range []time.Duration{0, -time.Second} {
		stop := keepalive.Start(interval, counting(&calls, nil))

		time.Sleep(5 * tick)
		stop()
	}

	require.Zero(t, calls.Load())
}
