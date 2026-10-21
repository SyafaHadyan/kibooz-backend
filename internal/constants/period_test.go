package constants_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

func TestParsePeriodAcceptsTheThreeNamesAndNothingElse(t *testing.T) {
	for raw, want := range map[string]constants.Period{
		"":      "",
		"today": constants.PeriodToday,
		"week":  constants.PeriodWeek,
		"month": constants.PeriodMonth,
	} {
		got, ok := constants.ParsePeriod(raw)

		require.True(t, ok, raw)
		require.Equal(t, want, got, raw)
	}

	for _, raw := range []string{"year", "Today", "WEEK", " month", "all", "semester"} {
		_, ok := constants.ParsePeriod(raw)

		require.False(t, ok, raw)
	}
}
