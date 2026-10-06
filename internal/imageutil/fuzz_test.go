package imageutil_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/imageutil"
)

const fuzzMaxBytes = 4096

func FuzzInspect(f *testing.F) {
	f.Add(pngBytes)
	f.Add([]byte("<html>not an image</html>"))
	f.Add([]byte{0xff, 0xd8, 0xff})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		image, err := imageutil.Inspect(data, fuzzMaxBytes)
		if err != nil {
			return
		}

		require.NotEmpty(t, data)
		require.LessOrEqual(t, len(image.Data), fuzzMaxBytes)
		require.Contains(t, []string{"image/jpeg", "image/png", "image/webp"}, image.ContentType)
		require.Contains(t, []string{".jpg", ".png", ".webp"}, image.Extension)
	})
}

func FuzzDecodeBase64(f *testing.F) {
	encoded := base64.StdEncoding.EncodeToString(pngBytes)

	f.Add(encoded)
	f.Add("data:image/png;base64," + encoded)
	f.Add("data:image/png;base64")
	f.Add("   " + encoded + "\n")
	f.Add("%%%not base64%%%")
	f.Add("")

	f.Fuzz(func(t *testing.T, value string) {
		image, err := imageutil.DecodeBase64(value, fuzzMaxBytes)
		if err != nil {
			return
		}

		require.NotEmpty(t, image.Data)
		require.LessOrEqual(t, len(image.Data), fuzzMaxBytes)
		require.Contains(t, []string{".jpg", ".png", ".webp"}, image.Extension)
	})
}
