package usecase

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
)

func TestCheckNames(t *testing.T) {
	guru := func(name string, class string) dto.RegisterRequest {
		return dto.RegisterRequest{Role: constants.RoleGuru, FullName: name, Class: &dto.RegisterClass{Name: class}}
	}

	wali := func(name string, child string) dto.RegisterRequest {
		return dto.RegisterRequest{Role: constants.RoleWali, FullName: name, Student: &dto.RegisterStudent{FullName: child}}
	}

	tests := map[string]struct {
		req  dto.RegisterRequest
		want map[string]string
	}{
		"good names":                               {guru("Bu Sari", "Class A"), map[string]string{}},
		"a name of two letters":                    {guru("Al", "A"), map[string]string{}},
		"a name with spaces around it":             {guru("  Bu Sari  ", " A "), map[string]string{}},
		"a name of letters with accents":           {wali("Éé", "Çç"), map[string]string{}},
		"a blank name":                             {guru("  ", "A"), map[string]string{"fullName": "is required"}},
		"a name that is empty":                     {guru("", "A"), map[string]string{"fullName": "is required"}},
		"one letter between spaces":                {guru(" A ", "A"), map[string]string{"fullName": "too short or too small, minimum 2"}},
		"a blank class name":                       {guru("Bu Sari", "   "), map[string]string{"class.name": "is required"}},
		"a blank class name with tabs":             {guru("Bu Sari", "\t\n"), map[string]string{"class.name": "is required"}},
		"a blank child name":                       {wali("Ibu Sari", "  "), map[string]string{"student.fullName": "is required"}},
		"a short child name":                       {wali("Ibu Sari", " B "), map[string]string{"student.fullName": "too short or too small, minimum 2"}},
		"everything blank":                         {wali(" ", " "), map[string]string{"fullName": "is required", "student.fullName": "is required"}},
		"the class of a parent is ignored":         {dto.RegisterRequest{Role: constants.RoleWali, FullName: "Ibu Sari", Class: &dto.RegisterClass{Name: " "}, Student: &dto.RegisterStudent{FullName: "Anak"}}, map[string]string{}},
		"a missing class is left to the validator": {dto.RegisterRequest{Role: constants.RoleGuru, FullName: "Bu Sari"}, map[string]string{}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.want, checkNames(tt.req))
		})
	}
}
