package imageutil_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/imageutil"
)

const (
	fuzzMaxBytes = 4096

	// a cleaned image can be larger than the one that was sent by the orientation block that replaces a bigger EXIF block
	fuzzOrientationBlock = 64
)

func FuzzInspect(f *testing.F) {
	f.Add(pngBytes)
	f.Add([]byte("<html>not an image</html>"))
	f.Add([]byte{0xff, 0xd8, 0xff})
	f.Add([]byte{0xff, 0xd8, 0xff, 0xe1, 0x00, 0x02, 0xff, 0xda, 0x00, 0x02, 0x01, 0xff, 0xd9})
	f.Add([]byte("RIFF\x16\x00\x00\x00WEBPVP8L\x04\x00\x00\x00abcd\x00\x00"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		image, err := imageutil.Inspect(data, fuzzMaxBytes)
		if err != nil {
			return
		}

		require.NotEmpty(t, data)
		require.LessOrEqual(t, len(image.Data), fuzzMaxBytes+fuzzOrientationBlock)
		require.Contains(t, []string{"image/jpeg", "image/png", "image/webp"}, image.ContentType)
		require.Contains(t, []string{".jpg", ".png", ".webp"}, image.Extension)

		// what is stored has no metadata left to remove, so cleaning it again changes nothing
		again, err := imageutil.Inspect(image.Data, fuzzMaxBytes+fuzzOrientationBlock)
		require.NoError(t, err)
		require.Equal(t, image.Data, again.Data)
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
		require.LessOrEqual(t, len(image.Data), fuzzMaxBytes+fuzzOrientationBlock)
		require.Contains(t, []string{".jpg", ".png", ".webp"}, image.Extension)
	})
}
