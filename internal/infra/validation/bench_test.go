package validation_test

import (
	"testing"

	"github.com/SyafaHadyan/kibooz-backend/internal/domain/dto"
	"github.com/SyafaHadyan/kibooz-backend/internal/infra/validation"
)

// Struct runs on the body of every request that has one
func BenchmarkStruct(b *testing.B) {
	request := dto.RegisterRequest{
		Email: "a@example.com", Password: "a long phrase nobody guesses", FullName: "Ana Maria", Role: "GURU", Class: &dto.RegisterClass{Name: "A"},
	}

	b.ReportAllocs()

	for range b.N {
		err := validation.Struct(&request)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIsCommonPassword(b *testing.B) {
	b.ReportAllocs()

	for range b.N {
		validation.IsCommonPassword("A Long Phrase Nobody Guesses")
	}
}
