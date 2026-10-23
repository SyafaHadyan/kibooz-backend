package imageutil_test

import (
	"encoding/base64"
	"testing"

	"github.com/SyafaHadyan/kibooz-backend/internal/imageutil"
)

// the benchmarks of the repository are the ones that the scheduled Benchmarks workflow compares between two commits, so a
// benchmark should keep its name and its meaning, see docs/testing.md

func BenchmarkInspect(b *testing.B) {
	b.ReportAllocs()

	for range b.N {
		_, err := imageutil.Inspect(pngBytes, 1<<20)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeBase64(b *testing.B) {
	encoded := base64.StdEncoding.EncodeToString(pngBytes)

	b.ReportAllocs()

	for range b.N {
		_, err := imageutil.DecodeBase64(encoded, 1<<20)
		if err != nil {
			b.Fatal(err)
		}
	}
}
