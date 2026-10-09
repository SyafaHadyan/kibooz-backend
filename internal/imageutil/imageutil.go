// Package imageutil checks the type and size of uploaded images and decodes their base64 form
package imageutil

import (
	"encoding/base64"
	"strings"

	"github.com/gabriel-vasile/mimetype"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
)

type Image struct {
	Data        []byte
	ContentType string
	Extension   string
}

var allowed = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// Inspect checks the real content type of data and its size against maxBytes
func Inspect(data []byte, maxBytes int) (Image, error) {
	if len(data) == 0 {
		return Image{}, apperror.ErrInvalidImage
	}

	if len(data) > maxBytes {
		return Image{}, apperror.ErrFileTooLarge
	}

	contentType := mimetype.Detect(data).String()
	if i := strings.Index(contentType, ";"); i >= 0 {
		contentType = contentType[:i]
	}

	extension, ok := allowed[contentType]
	if !ok {
		return Image{}, apperror.ErrInvalidImage
	}

	return Image{Data: data, ContentType: contentType, Extension: extension}, nil
}

// DecodeBase64 accepts either a data URI or bare base64 and returns the checked image
func DecodeBase64(value string, maxBytes int) (Image, error) {
	payload := strings.TrimSpace(value)

	if strings.HasPrefix(payload, "data:") {
		_, rest, found := strings.Cut(payload, ",")
		if !found {
			return Image{}, apperror.ErrInvalidImage
		}

		payload = rest
	}

	// the decoded size is about three quarters of the encoded size, so reject early
	if len(payload) > maxBytes*4/3+8 {
		return Image{}, apperror.ErrFileTooLarge
	}

	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return Image{}, apperror.ErrInvalidImage
	}

	return Inspect(data, maxBytes)
}
