// Package validation binds JSON bodies and turns validator failures into API errors
package validation

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v3"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
)

var (
	once     sync.Once
	instance *validator.Validate
)

func get() *validator.Validate {
	once.Do(func() {
		instance = validator.New(validator.WithRequiredStructEnabled())
		instance.RegisterTagNameFunc(func(field reflect.StructField) string {
			name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
			if name == "-" || name == "" {
				return field.Name
			}

			return name
		})
	})

	return instance
}

// BindBody decodes the JSON body into dst and validates it
func BindBody(c fiber.Ctx, dst any) error {
	body := c.Body()
	if len(body) == 0 {
		return apperror.Validation(map[string]string{"body": "wajib diisi"})
	}

	err := json.Unmarshal(body, dst)
	if err != nil {
		return apperror.Validation(map[string]string{"body": "format JSON tidak valid"})
	}

	return Struct(dst)
}

// Struct validates dst and returns an application error describing every failed field
func Struct(dst any) error {
	err := get().Struct(dst)
	if err == nil {
		return nil
	}

	var validationErrors validator.ValidationErrors
	if !errors.As(err, &validationErrors) {
		return apperror.Internal(err)
	}

	details := make(map[string]string, len(validationErrors))
	for _, fieldErr := range validationErrors {
		details[fieldPath(fieldErr)] = describe(fieldErr)
	}

	return apperror.Validation(details)
}

func fieldPath(fieldErr validator.FieldError) string {
	path := fieldErr.Namespace()

	if _, rest, found := strings.Cut(path, "."); found {
		return rest
	}

	return path
}

func describe(fieldErr validator.FieldError) string {
	switch fieldErr.Tag() {
	case "required", "required_if":
		return "wajib diisi"
	case "email":
		return "format email tidak valid"
	case "min":
		return "terlalu pendek atau terlalu kecil, minimal " + fieldErr.Param()
	case "max":
		return "terlalu panjang atau terlalu besar, maksimal " + fieldErr.Param()
	case "oneof":
		return "harus salah satu dari " + fieldErr.Param()
	case "numeric":
		return "hanya boleh berisi angka"
	case "alphanum":
		return "hanya boleh berisi huruf dan angka"
	case "gte", "lte":
		return "di luar rentang yang diizinkan"
	default:
		return "tidak valid"
	}
}
