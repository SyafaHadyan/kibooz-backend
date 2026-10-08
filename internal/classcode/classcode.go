// Package classcode stores a class under a random join code that no other class uses
package classcode

import (
	"crypto/rand"
	"errors"
	"math/big"

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
	for range retries {
		code, err := newCode()
		if err != nil {
			return err
		}

		class.JoinCode = code

		// a savepoint keeps the outer transaction usable after a unique violation
		err = tx.Transaction(func(inner *gorm.DB) error {
			return inner.Create(class).Error
		})
		if err == nil {
			return nil
		}

		if !isCodeTaken(err) {
			return err
		}
	}

	return errors.New("failed to generate a unique class code")
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
