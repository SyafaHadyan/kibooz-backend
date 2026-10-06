package validation_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
)

// FuzzStruct decodes arbitrary JSON into every request body and checks that validation never panics
// and only ever answers with a 400 describing the fields, never an internal error.
func FuzzStruct(f *testing.F) {
	f.Add(`{"email":"a@b.co","password":"12345678","fullName":"Ana","role":"GURU","class":{"name":"A"}}`)
	f.Add(`{"email":"a@b.co","password":"12345678","role":"WALI","classCode":"ABCD12","student":{"fullName":"Budi"}}`)
	f.Add(`{"studentId":"not-a-uuid","moodType":"SENANG","confidenceScore":2}`)
	f.Add(`{"refreshToken":""}`)
	f.Add(`{}`)
	f.Add(`null`)
	f.Add(`[]`)

	f.Fuzz(func(t *testing.T, body string) {
		targets := []any{
			new(dto.RegisterRequest),
			new(dto.LoginRequest),
			new(dto.RefreshRequest),
			new(dto.LogoutRequest),
			new(dto.DeleteAccountRequest),
			new(dto.ScanClaimRequest),
			new(dto.LogMoodRequest),
			new(dto.ApplyGuidanceRequest),
		}

		for _, target := range targets {
			if json.Unmarshal([]byte(body), target) != nil {
				continue
			}

			err := validation.Struct(target)
			if err == nil {
				continue
			}

			appErr := apperror.As(err)
			require.Equal(t, http.StatusBadRequest, appErr.Status)
			require.NotEmpty(t, appErr.Details)
		}
	})
}
