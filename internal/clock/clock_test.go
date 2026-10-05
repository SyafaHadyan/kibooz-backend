package clock_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/clock"
)

func jakarta(t *testing.T) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)

	return loc
}

func TestDayBoundsUsesLocalMidnight(t *testing.T) {
	loc := jakarta(t)

	// 20:00 UTC on 3 Oct is already 4 Oct in Jakarta
	from, to := clock.DayBounds(time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC), loc)

	require.Equal(t, time.Date(2026, 10, 3, 17, 0, 0, 0, time.UTC), from)
	require.Equal(t, time.Date(2026, 10, 4, 17, 0, 0, 0, time.UTC), to)
}

func TestWeekBoundsStartOnMonday(t *testing.T) {
	loc := jakarta(t)

	// 4 Oct 2026 is a Sunday so its week started on Monday 28 Sep
	from, to := clock.WeekBounds(time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC), loc)

	require.Equal(t, time.Date(2026, 9, 27, 17, 0, 0, 0, time.UTC), from)
	require.Equal(t, time.Date(2026, 10, 4, 17, 0, 0, 0, time.UTC), to)

	// Monday itself stays in the same week
	from, _ = clock.WeekBounds(time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC), loc)
	require.Equal(t, time.Date(2026, 9, 27, 17, 0, 0, 0, time.UTC), from)
}

func TestMonthBounds(t *testing.T) {
	loc := jakarta(t)

	from, to := clock.MonthBounds(time.Date(2026, 12, 31, 18, 0, 0, 0, time.UTC), loc)

	// 18:00 UTC on 31 Dec is 1 Jan 2027 in Jakarta
	require.Equal(t, time.Date(2026, 12, 31, 17, 0, 0, 0, time.UTC), from)
	require.Equal(t, time.Date(2027, 1, 31, 17, 0, 0, 0, time.UTC), to)
}
