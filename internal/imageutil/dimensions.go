package imageutil

import (
	"bytes"
	"encoding/binary"
	"image"

	// these register the decoders that image.DecodeConfig uses, which read the size of an image without decoding its pixels
	_ "image/jpeg"
	_ "image/png"
)

const (
	// MaxSide is the longest side of an image in pixels, and MaxPixels the number of pixels in all. A file of a few MiB can
	// describe an image of billions of pixels, and every app that shows it has to allocate memory for all of them.
	MaxSide   = 8192
	MaxPixels = 40_000_000
)

type size struct{ width, height int }

func (s size) tooLarge() bool {
	return s.width < 1 || s.height < 1 || s.width > MaxSide || s.height > MaxSide || s.width*s.height > MaxPixels
}

// sizes reads the sizes that the headers of an image announce, without decoding it. A WebP file can announce more than
// one, a canvas and the image inside it, and each of them has to be within the limits.
func sizes(contentType string, data []byte) ([]size, error) {
	if contentType == "image/webp" {
		return webpSizes(data)
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errMalformed
	}

	return []size{{config.Width, config.Height}}, nil
}

// webpMaxField is the largest value of a 14 bit field
const webpMaxField = 0x3fff

// webpSizes reads the canvas of the extended header and the size of a lossy or lossless image chunk. The data was
// checked by stripWebP, so the chunks add up to the size of the file, and only their contents need a look.
func webpSizes(data []byte) ([]size, error) {
	if len(data) < webpHeaderSize {
		return nil, errMalformed
	}

	var found []size

	for i := webpHeaderSize; i+webpChunkHeader <= len(data); {
		fourCC := string(data[i : i+4])
		length := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))

		end := i + webpChunkHeader + length + length%2
		if length < 0 || end > len(data) {
			return nil, errMalformed
		}

		payload := data[i+webpChunkHeader : i+webpChunkHeader+length]

		switch fourCC {
		case "VP8X":
			if len(payload) < webpExtendedHeaderSize {
				return nil, errMalformed
			}

			// the canvas is stored as its size minus one, in three bytes each
			found = append(found, size{
				width:  1 + (int(payload[4]) | int(payload[5])<<8 | int(payload[6])<<16),
				height: 1 + (int(payload[7]) | int(payload[8])<<8 | int(payload[9])<<16),
			})
		case "VP8 ":
			// the lossy frame has a tag of three bytes and a start code, then the 14 bit sizes
			if len(payload) < 10 || payload[3] != 0x9d || payload[4] != 0x01 || payload[5] != 0x2a {
				return nil, errMalformed
			}

			found = append(found, size{
				width:  int(binary.LittleEndian.Uint16(payload[6:8])) & webpMaxField,
				height: int(binary.LittleEndian.Uint16(payload[8:10])) & webpMaxField,
			})
		case "VP8L":
			// the lossless header is a signature byte and 14 bit sizes stored as the size minus one
			if len(payload) < 5 || payload[0] != 0x2f {
				return nil, errMalformed
			}

			bits := binary.LittleEndian.Uint32(payload[1:5])
			found = append(found, size{width: int(bits&webpMaxField) + 1, height: int(bits>>14&webpMaxField) + 1})
		}

		i = end
	}

	if len(found) == 0 {
		return nil, errMalformed
	}

	return found, nil
}
