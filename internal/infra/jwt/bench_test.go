package jwt

import (
	"testing"

	"github.com/google/uuid"

	"github.com/SyafaHadyan/kibooz-backend/internal/constants"
)

func BenchmarkGenerateToken(b *testing.B) {
	service := newService("0123456789abcdef0123456789abcdef", 60)
	userID := uuid.New()

	b.ReportAllocs()

	for range b.N {
		_, err := service.GenerateToken(userID, constants.RoleWali)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// ValidateToken runs on every request of a signed in account
func BenchmarkValidateToken(b *testing.B) {
	service := newService("0123456789abcdef0123456789abcdef", 60)

	token, err := service.GenerateToken(uuid.New(), constants.RoleWali)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for range b.N {
		_, err := service.ValidateToken(token)
		if err != nil {
			b.Fatal(err)
		}
	}
}
