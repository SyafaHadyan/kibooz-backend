// Package clock computes calendar boundaries in the school timezone
package clock

import (
	"time"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

// PeriodBounds returns the start of the local day, week or month of a period that contains t and the start of the next one
func PeriodBounds(period constants.Period, t time.Time, loc *time.Location) (time.Time, time.Time) {
	switch period {
	case constants.PeriodWeek:
		return WeekBounds(t, loc)
	case constants.PeriodMonth:
		return MonthBounds(t, loc)
	default:
		return DayBounds(t, loc)
	}
}

// DayBounds returns the start of the local day containing t and the start of the next one
func DayBounds(t time.Time, loc *time.Location) (time.Time, time.Time) {
	local := t.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)

	return start.UTC(), start.AddDate(0, 0, 1).UTC()
}

// WeekBounds returns Monday 00:00 of the local week containing t and the following Monday
func WeekBounds(t time.Time, loc *time.Location) (time.Time, time.Time) {
	local := t.In(loc)
	offset := (int(local.Weekday()) + 6) % 7
	start := time.Date(local.Year(), local.Month(), local.Day()-offset, 0, 0, 0, 0, loc)

	return start.UTC(), start.AddDate(0, 0, 7).UTC()
}

// MonthBounds returns the first instant of the local month containing t and of the next month
func MonthBounds(t time.Time, loc *time.Location) (time.Time, time.Time) {
	local := t.In(loc)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)

	return start.UTC(), start.AddDate(0, 1, 0).UTC()
}
