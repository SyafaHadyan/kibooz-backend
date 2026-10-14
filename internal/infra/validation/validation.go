// Package validation binds JSON bodies and turns validator failures into API errors
package validation

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
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
	// PostgreSQL cannot store a NUL character in text, so it has to be refused here and not left to fail in a query as a 500
	if found := nulFields(dst); len(found) > 0 {
		return apperror.Validation(found)
	}

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

// nulFields lists the text fields of dst that hold a NUL character, which JSON can carry as \u0000
func nulFields(dst any) map[string]string {
	found := map[string]string{}

	value := reflect.ValueOf(dst)
	if value.Kind() == reflect.Pointer && !value.IsNil() {
		value = value.Elem()
	}

	if value.Kind() == reflect.Struct {
		for i := range value.NumField() {
			field := value.Type().Field(i)
			if field.IsExported() {
				collectNul(value.Field(i), fieldName(field), found)
			}
		}
	} else {
		collectNul(value, "body", found)
	}

	return found
}

func fieldName(field reflect.StructField) string {
	name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
	if name == "-" || name == "" {
		return field.Name
	}

	return name
}

func collectNul(value reflect.Value, path string, found map[string]string) {
	const message = "must not contain a null character"

	switch value.Kind() {
	case reflect.String:
		if strings.ContainsRune(value.String(), 0) {
			found[path] = message
		}
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			collectNul(value.Elem(), path, found)
		}
	case reflect.Struct:
		for i := range value.NumField() {
			field := value.Type().Field(i)
			if field.IsExported() {
				collectNul(value.Field(i), path+"."+fieldName(field), found)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := range value.Len() {
			collectNul(value.Index(i), path+"["+strconv.Itoa(i)+"]", found)
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			if key.Kind() == reflect.String && strings.ContainsRune(key.String(), 0) {
				found[path] = message
			}

			collectNul(value.MapIndex(key), path+"."+key.String(), found)
		}
	default:
	}
}
