package imageutil_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
	"github.com/SyafaHadyan/kibooz-backend/internal/imageutil"
)

// smallest valid PNG, a single transparent pixel
var pngBytes, _ = base64.StdEncoding.DecodeString(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
)

func TestInspectAcceptsPNG(t *testing.T) {
	image, err := imageutil.Inspect(pngBytes, 1024)

	require.NoError(t, err)
	require.Equal(t, "image/png", image.ContentType)
	require.Equal(t, ".png", image.Extension)
}

func TestInspectRejectsNonImage(t *testing.T) {
	_, err := imageutil.Inspect([]byte("<html>not an image</html>"), 1024)

	require.ErrorIs(t, err, apperror.ErrInvalidImage)
}

func TestInspectRejectsOversizedAndEmpty(t *testing.T) {
	_, err := imageutil.Inspect(pngBytes, 10)
	require.ErrorIs(t, err, apperror.ErrFileTooLarge)

	_, err = imageutil.Inspect(nil, 10)
	require.ErrorIs(t, err, apperror.ErrInvalidImage)
}

func TestDecodeBase64HandlesDataURIAndBareBase64(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(pngBytes)

	withPrefix, err := imageutil.DecodeBase64("data:image/png;base64,"+encoded, 1024)
	require.NoError(t, err)
	require.Equal(t, "image/png", withPrefix.ContentType)

	bare, err := imageutil.DecodeBase64(encoded, 1024)
	require.NoError(t, err)
	require.Equal(t, pngBytes, bare.Data)
}

func TestDecodeBase64RejectsGarbage(t *testing.T) {
	_, err := imageutil.DecodeBase64("data:image/png;base64,@@@not-base64@@@", 1024)
	require.ErrorIs(t, err, apperror.ErrInvalidImage)

	_, err = imageutil.DecodeBase64("data:image/png", 1024)
	require.ErrorIs(t, err, apperror.ErrInvalidImage)
}
