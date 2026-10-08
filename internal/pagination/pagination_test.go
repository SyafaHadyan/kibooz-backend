package pagination

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
)

func TestParseFallsBackToTheDefaults(t *testing.T) {
	params, err := parse("", "")

	require.NoError(t, err)
	require.Equal(t, Params{Page: 1, Limit: DefaultLimit}, params)
	require.Zero(t, params.Offset())
}

func TestParseReadsBothValues(t *testing.T) {
	params, err := parse("3", "10")

	require.NoError(t, err)
	require.Equal(t, Params{Page: 3, Limit: 10}, params)
	require.Equal(t, 20, params.Offset())
}

func TestParseAcceptsTheLimits(t *testing.T) {
	params, err := parse("10000", "50")

	require.NoError(t, err)
	require.Equal(t, Params{Page: MaxPage, Limit: MaxLimit}, params)
}

func TestParseReportsEveryInvalidValue(t *testing.T) {
	tests := []struct {
		name  string
		page  string
		limit string
		keys  []string
	}{
		{name: "page below one", page: "0", keys: []string{"page"}},
		{name: "page above the maximum", page: "10001", keys: []string{"page"}},
		{name: "page is not a number", page: "two", keys: []string{"page"}},
		{name: "limit below one", limit: "0", keys: []string{"limit"}},
		{name: "limit above the maximum", limit: "51", keys: []string{"limit"}},
		{name: "limit is not a number", limit: "ten", keys: []string{"limit"}},
		{name: "both are invalid", page: "-1", limit: "-1", keys: []string{"page", "limit"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parse(test.page, test.limit)

			var appErr *apperror.Error

			require.True(t, errors.As(err, &appErr))
			require.Equal(t, "VALIDATION_ERROR", appErr.Code)
			require.Len(t, appErr.Details, len(test.keys))

			for _, key := range test.keys {
				require.Contains(t, appErr.Details, key)
			}
		})
	}
}
