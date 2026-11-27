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

func TestSubjectCodesAreTheOnesTheMessageNames(t *testing.T) {
	codes := constants.SubjectCodes()

	require.Len(t, codes, 6)
	require.Equal(t, "must be one of SENI-A1, BHS-A1, LOG-A1, MOT-A1, AGM-A1, SOS-A1", constants.SubjectCodeMessage())

	for _, code := range codes {
		require.True(t, code.Valid(), string(code))
		require.Contains(t, constants.SubjectCodeMessage(), string(code))
	}

	codes[0] = "CHANGED"

	require.Equal(t, constants.SubjectSeni, constants.SubjectCodes()[0], "the list a caller gets is a copy")
	require.False(t, constants.SubjectCode("").Valid())
}
