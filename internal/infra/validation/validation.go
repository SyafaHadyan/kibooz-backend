// Package validation binds JSON bodies and turns validator failures into API errors
package validation

import (
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v3"
	"golang.org/x/text/unicode/norm"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
)

// PhonePattern accepts 6 to 30 characters of digits with an optional leading plus, and spaces, dashes and brackets between them
var PhonePattern = regexp.MustCompile(`^\+?\(?[0-9][0-9 ()-]{4,26}[0-9]$`)

// notCommonTag is the validator tag that refuses a password from the list of common passwords
const notCommonTag = "notcommon"

//go:embed common-passwords.txt
var commonPasswordList string

var (
	commonOnce      sync.Once
	commonPasswords map[string]struct{}
)

// IsCommonPassword reports whether the password is on the embedded list of passwords that people choose most, which an attacker
// tries first. The list is lowercase, so the case of the password does not matter. It holds no password below the minimum length
// of 8, because the length rule refuses those first.
func IsCommonPassword(password string) bool {
	commonOnce.Do(func() {
		lines := strings.Fields(commonPasswordList)
		commonPasswords = make(map[string]struct{}, len(lines))

		for _, line := range lines {
			commonPasswords[line] = struct{}{}
		}
	})

	_, found := commonPasswords[strings.ToLower(password)]

	return found
}

// OwnDetailsMessage is what a refused password is told, for the field that holds it
const OwnDetailsMessage = "must not contain your email, name, phone number or ID number"

// minIdentityRunes is the shortest piece of a person's own details that a password may not contain. A shorter piece, such as the
// first letters of an email, would make the rule refuse passwords that have nothing to do with the person.
const minIdentityRunes = 6

// ContainsIdentity reports whether the password contains one of the details of the person who sets it, such as the email, the part of
// the email before the at sign, a phone number, a NIP or a NISN. These are the first guesses of someone who knows the person, and a
// parent may well use the NISN of the child. The whole detail has to be inside the password. The case, the spaces and the signs of
// a name or a phone number do not matter, and the empty details and the ones shorter than six characters are left out.
func ContainsIdentity(password string, identities ...string) bool {
	plain := identityForm(password)

	for _, identity := range identities {
		candidates := []string{identity}
		if local, _, found := strings.Cut(identity, "@"); found {
			candidates = append(candidates, local)
		}

		for _, candidate := range candidates {
			form := identityForm(candidate)
			if utf8.RuneCountInString(form) >= minIdentityRunes && strings.Contains(plain, form) {
				return true
			}
		}
	}

	return false
}

// identityForm lowercases the text and keeps only its letters and digits, so the spaces, the commas of a title such as "S.Pd.", the
// apostrophes and the signs of a phone number do not matter. The text is composed first (NFC), so an accented letter that is written as
// a letter and a combining mark matches the same letter in one piece, and the mark is not dropped from only one of them.
func identityForm(text string) string {
	return strings.Map(func(r rune) rune {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return -1
		}

		return unicode.ToLower(r)
	}, norm.NFC.String(text))
}

var (
	once     sync.Once
	instance *validator.Validate
)

func get() *validator.Validate {
	once.Do(func() {
		instance = validator.New(validator.WithRequiredStructEnabled())

		err := instance.RegisterValidation(notCommonTag, func(fl validator.FieldLevel) bool {
			return !IsCommonPassword(fl.Field().String())
		})
		if err != nil {
			panic(err)
		}

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
	case notCommonTag:
		return "is too common, choose another password"
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
