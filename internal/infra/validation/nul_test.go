package validation_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
)

func TestANulCharacterIsRefusedWithTheNameOfTheField(t *testing.T) {
	t.Run("a field of the body", func(t *testing.T) {
		err := validation.Struct(&dto.CreateThreadRequest{Title: "ok", Body: "bad\x00body"})
		require.Error(t, err)

		appErr := apperror.As(err)
		require.Equal(t, http.StatusBadRequest, appErr.Status)
		require.Equal(t, map[string]string{"body": "must not contain a null character"}, appErr.Details)
	})

	t.Run("a field of a nested object", func(t *testing.T) {
		err := validation.Struct(&dto.RegisterRequest{
			Email: "a@b.co", Password: "12345678", FullName: "Ana", Role: "WALI", ClassCode: "ABCD2345",
			Student: &dto.RegisterStudent{NISN: "12345", FullName: "Budi\x00"},
		})
		require.Error(t, err)
		require.Contains(t, apperror.As(err).Details, "student.fullName")
	})

	t.Run("every field that has one is named", func(t *testing.T) {
		err := validation.Struct(&dto.CreateThreadRequest{Title: "a\x00", Body: "b\x00"})
		require.Error(t, err)
		require.Len(t, apperror.As(err).Details, 2)
	})

	t.Run("lists, maps and pointers", func(t *testing.T) {
		type nested struct {
			Names  []string          `json:"names"`
			Labels map[string]string `json:"labels"`
			Note   *string           `json:"note"`
		}

		note := "x\x00"

		err := validation.Struct(&nested{Names: []string{"ok", "no\x00"}, Labels: map[string]string{"k": "v\x00", "a\x00": "v"}, Note: &note})
		require.Error(t, err)
		require.Contains(t, apperror.As(err).Details, "names[1]")
		require.Contains(t, apperror.As(err).Details, "labels.k")
		require.Contains(t, apperror.As(err).Details, "labels")
		require.Contains(t, apperror.As(err).Details, "note")
	})

	t.Run("a body that is not a struct", func(t *testing.T) {
		list := []string{"ok", "no\x00"}

		require.Error(t, validation.Struct(&list))
	})
}

func TestTextWithoutANulCharacterIsNotAffected(t *testing.T) {
	text := "Ibu Siti, \"kelas\" 1 \\u0000 é世界\n"

	err := validation.Struct(&dto.CreateThreadRequest{Title: text, Body: text})
	require.NoError(t, err)
}
