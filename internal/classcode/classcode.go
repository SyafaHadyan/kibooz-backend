// Package classcode stores a class under a random join code that no other class uses
package classcode

import (
	"crypto/rand"
	"errors"
	"math/big"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/SyafaHadyan/kibooz-backend/internal/domain/entity"
)

const (
	// the alphabet leaves out the characters that are easy to confuse when a code is read aloud or copied by hand
	alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	length   = 6
	retries  = 8

	uniqueViolation = "23505"
	codeConstraint  = "classes_join_code_key"
)

// Create retries with a new random code when it collides with an existing class
func Create(tx *gorm.DB, class *entity.Class) error {
	_, err := withNewCode(tx, func(inner *gorm.DB, code string) error {
		class.JoinCode = code

		return inner.Create(class).Error
	})

	return err
}

// Replace gives a class a new random code and returns it, so the old code stops working at once
func Replace(tx *gorm.DB, classID uuid.UUID) (string, error) {
	return withNewCode(tx, func(inner *gorm.DB, code string) error {
		result := inner.Model(&entity.Class{}).Where("id = ?", classID).Update("join_code", code)
		if result.Error != nil {
			return result.Error
		}

		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		return nil
	})
}

// withNewCode runs store with a random code and tries again with another one when the code is already taken
func withNewCode(tx *gorm.DB, store func(inner *gorm.DB, code string) error) (string, error) {
	for range retries {
		code, err := newCode()
		if err != nil {
			return "", err
		}

		// a savepoint keeps the outer transaction usable after a unique violation
		err = tx.Transaction(func(inner *gorm.DB) error {
			return store(inner, code)
		})
		if err == nil {
			return code, nil
		}

		if !isCodeTaken(err) {
			return "", err
		}
	}

	return "", errors.New("failed to generate a unique class code")
}

func newCode() (string, error) {
	code := make([]byte, length)
	limit := big.NewInt(int64(len(alphabet)))

	for i := range code {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}

		code[i] = alphabet[n.Int64()]
	}

	return string(code), nil
}

func isCodeTaken(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == codeConstraint
}
