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
		return apperror.Validation(map[string]string{"body": "is required"})
	}

	err := json.Unmarshal(body, dst)
	if err != nil {
		return apperror.Validation(map[string]string{"body": "invalid JSON format"})
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
		return "is required"
	case "email":
		return "invalid email format"
	case "min":
		return "too short or too small, minimum " + fieldErr.Param()
	case "max":
		return "too long or too large, maximum " + fieldErr.Param()
	case "oneof":
		return "must be one of " + fieldErr.Param()
	case "numeric", "number":
		return "must contain digits only"
	case "alphanum":
		return "must contain letters and digits only"
	case "gte", "lte":
		return "is outside the allowed range"
	default:
		return "is invalid"
	}
}
