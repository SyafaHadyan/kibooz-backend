package constants_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kiboozapp/kibooz-backend/internal/constants"
)

func TestParseSubjectCodeAcceptsTheSixSubjectsAndEmptyOnly(t *testing.T) {
	for raw, want := range map[string]constants.SubjectCode{
		"SENI-A1": constants.SubjectSeni,
		"BHS-A1":  constants.SubjectBahasa,
		"LOG-A1":  constants.SubjectLogika,
		"MOT-A1":  constants.SubjectMotorik,
		"AGM-A1":  constants.SubjectAgama,
		"SOS-A1":  constants.SubjectSosial,
	} {
		got, ok := constants.ParseSubjectCode(raw)

		require.True(t, ok, raw)
		require.NotNil(t, got, raw)
		require.Equal(t, want, *got, raw)
	}

	got, ok := constants.ParseSubjectCode("")
	require.True(t, ok)
	require.Nil(t, got)

	for _, raw := range []string{"seni-a1", "SENI", "GENERAL", " SENI-A1", "ART-A1"} {
		parsed, valid := constants.ParseSubjectCode(raw)

		require.False(t, valid, raw)
		require.Nil(t, parsed, raw)
	}
}
