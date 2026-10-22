package sessions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAZeroIntervalSwitchesTheSweepOff(t *testing.T) {
	stop := NewSweeper(nil, time.Hour).Start(0)

	require.NotPanics(t, stop, "there is nothing running to wait for")
}
