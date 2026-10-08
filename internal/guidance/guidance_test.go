package guidance_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/guidance"
)

func TestBannerURL(t *testing.T) {
	item, ok := guidance.ForMood(constants.MoodBingung)
	require.True(t, ok)

	tests := map[string]struct {
		base string
		want string
	}{
		"a public base":            {"https://cdn.example.com", "https://cdn.example.com/guidance/banner_aman.png"},
		"a base with a trailing /": {"https://cdn.example.com/", "https://cdn.example.com/guidance/banner_aman.png"},
		"no public base":           {"", ""},
		"a base that is only a /":  {"/", ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.want, item.BannerURL(tt.base))
		})
	}
}

func TestEveryMoodHasAGuideThatExists(t *testing.T) {
	for _, mood := range []constants.Mood{constants.MoodSenang, constants.MoodSedih, constants.MoodMarah, constants.MoodBingung} {
		item, ok := guidance.ForMood(mood)

		require.True(t, ok, mood)
		require.True(t, guidance.Exists(item.ID), mood)
	}

	require.False(t, guidance.Exists("guidance-unknown"))
}
