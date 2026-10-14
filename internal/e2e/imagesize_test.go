package e2e

import (
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// pngHeaderOfSize is a PNG whose header announces a size and that holds no pixels, which is all the API reads of it
func pngHeaderOfSize(width int, height int) []byte {
	chunk := func(kind string, payload []byte) []byte {
		out := binary.BigEndian.AppendUint32(nil, uint32(len(payload))) //nolint:gosec // the payloads are a few bytes
		out = append(out, kind...)
		out = append(out, payload...)

		return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(append([]byte(kind), payload...)))
	}

	header := binary.BigEndian.AppendUint32(nil, uint32(width))    //nolint:gosec // see above
	header = binary.BigEndian.AppendUint32(header, uint32(height)) //nolint:gosec // see above
	header = append(header, 8, 2, 0, 0, 0)

	out := []byte("\x89PNG\r\n\x1a\n")
	out = append(out, chunk("IHDR", header)...)
	out = append(out, chunk("IDAT", nil)...)

	return append(out, chunk("IEND", nil)...)
}

// TestAnImageWithTooManyPixelsIsRefused sends a few dozen bytes that announce an image of ten billion pixels, which the
// apps would have to allocate memory for when they show it
func TestAnImageWithTooManyPixelsIsRefused(t *testing.T) {
	guru := registerGuru(t, "Pixels")
	_, code := classOf(t, guru)
	wali, studentID := registerWali(t, code, "Pixel Child")

	bomb := pngHeaderOfSize(100000, 100000)

	t.Run("an avatar", func(t *testing.T) {
		stored := len(uploaded("avatars/"))

		upload(t, wali.Token, map[string]string{"studentId": studentID}, "bomb.png", bomb).
			requireError(t, http.StatusBadRequest, "IMAGE_DIMENSIONS_TOO_LARGE")
		require.Len(t, uploaded("avatars/"), stored, "a refused upload must not leave a file behind")
	})

	t.Run("a trash photo", func(t *testing.T) {
		call(t, http.MethodPost, "/api/v1/trash/scan-claim", wali.Token, map[string]any{
			"studentId": studentID, "trashType": "ORGANIK", "confidenceScore": 0.9,
			"photoBase64": base64.StdEncoding.EncodeToString(bomb),
		}).requireError(t, http.StatusBadRequest, "IMAGE_DIMENSIONS_TOO_LARGE")
	})

	t.Run("a header with a size that is allowed passes", func(t *testing.T) {
		res := upload(t, wali.Token, map[string]string{"studentId": studentID}, "fine.png", pngHeaderOfSize(4000, 3000))
		require.Equal(t, http.StatusOK, res.Status, "body %v", res.Body)
	})
}
