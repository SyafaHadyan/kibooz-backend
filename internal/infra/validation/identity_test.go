package validation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPasswordWithThePersonsOwnDetailsIsRefused(t *testing.T) {
	cases := []struct {
		name       string
		password   string
		identities []string
	}{
		{"the whole email", "budi.santoso@example.com", []string{"budi.santoso@example.com"}},
		{"the part before the at sign", "Budi.Santoso-2026!", []string{"budi.santoso@example.com"}},
		{"in capitals", "BUDI.SANTOSO!!", []string{"budi.santoso@example.com"}},
		{"the phone number without its signs", "call08123456789x", []string{"0812-3456-789"}},
		{"the phone number with its signs", "call0812-3456-789x", []string{"+0812 3456 789"}},
		{"the nisn of the child", "x0054321987y", []string{"0054321987"}},
		{"the name without spaces", "ibuBudiSantoso99", []string{"Budi Santoso"}},
		{"a name with a title and commas", "SitiRahayuSPd1985!", []string{"Siti Rahayu, S.Pd."}},
		{"a name with an apostrophe", "xx-OBrien-Mc9", []string{"O'Brien Mc"}},
		{"a name with accents", "Zoë-Müller-77", []string{"Zoë Müller"}},
	}

	for _, test := range cases {
		require.True(t, ContainsIdentity(test.password, test.identities...), test.name)
	}
}

func TestAPasswordWithoutTheDetailsOrWithOnlyAShortPieceIsKept(t *testing.T) {
	require.False(t, ContainsIdentity("a-long-unrelated-passphrase", "budi.santoso@example.com", "0812345678", "Budi Santoso"))
	require.False(t, ContainsIdentity("budi1234xyz", "budi@example.com"), "a part of four characters is too short to count")
	require.False(t, ContainsIdentity("anything-at-all", "", "  ", "abc"), "empty and short details are left out")
	require.False(t, ContainsIdentity("anything-at-all", "-----", "()()()()()()"), "a detail of signs only becomes empty and is left out")
	require.False(t, ContainsIdentity("budisa-Xk29!", "budi.santoso@example.com"), "a piece of a detail is not the detail")
}
