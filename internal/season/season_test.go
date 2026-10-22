package season_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/season"
)

func TestLabelNamesTheSemesterByItsFirstDay(t *testing.T) {
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)

	utc := time.UTC
	both := []int{1, 7}

	tests := []struct {
		name   string
		at     time.Time
		loc    *time.Location
		months []int
		want   string
	}{
		{"the first day of the first semester", time.Date(2026, 1, 1, 0, 0, 0, 0, jakarta), jakarta, both, "2026-01"},
		{"the last day before the second semester", time.Date(2026, 6, 30, 23, 59, 59, 0, jakarta), jakarta, both, "2026-01"},
		{"the first day of the second semester", time.Date(2026, 7, 1, 0, 0, 0, 0, jakarta), jakarta, both, "2026-07"},
		{"the end of the year", time.Date(2026, 12, 31, 12, 0, 0, 0, jakarta), jakarta, both, "2026-07"},
		{"the order of the months does not matter", time.Date(2026, 8, 5, 0, 0, 0, 0, utc), utc, []int{7, 1}, "2026-07"},
		{"a single start month spans the year change", time.Date(2027, 3, 1, 0, 0, 0, 0, utc), utc, []int{9}, "2026-09"},
		{"before the first start month of the year", time.Date(2026, 1, 15, 0, 0, 0, 0, utc), utc, []int{3, 9}, "2025-09"},
		{"three semesters", time.Date(2026, 5, 20, 0, 0, 0, 0, utc), utc, []int{1, 5, 9}, "2026-05"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, season.Label(tt.at, tt.loc, tt.months))
		})
	}
}

func TestLabelFollowsTheTimeZoneOfTheSchoolAndNotUTC(t *testing.T) {
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)

	// 17:30 UTC on 30 June is 00:30 on 1 July in Jakarta, so the new semester has begun there
	at := time.Date(2026, 6, 30, 17, 30, 0, 0, time.UTC)

	require.Equal(t, "2026-07", season.Label(at, jakarta, []int{1, 7}))
	require.Equal(t, "2026-01", season.Label(at, time.UTC, []int{1, 7}))
}

func TestAnIntervalOfZeroSwitchesTheJobOff(t *testing.T) {
	stop := season.New(nil, nil, time.UTC, []int{1, 7}).Start(0)

	require.NotNil(t, stop)

	stop()
}
