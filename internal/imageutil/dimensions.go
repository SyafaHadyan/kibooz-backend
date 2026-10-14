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

// webpFrameHeaderSize is the position, the size, the duration and the flags of a frame in an animation, and its image
// chunks follow
const webpFrameHeaderSize = 16

// webpSizes reads the canvas of the extended header, the size of every frame of an animation and the size of every lossy
// or lossless image chunk, including the ones inside a frame. A file can announce a different size in each of those
// places, and a decoder may believe any of them, so each one has to be within the limits. The data was checked by
// stripWebP, so the chunks add up to the size of the file, and only their contents need a look.
func webpSizes(data []byte) ([]size, error) {
	if len(data) < webpHeaderSize {
		return nil, errMalformed
	}

	found, err := webpChunkSizes(data[webpHeaderSize:])
	if err != nil {
		return nil, err
	}

	if len(found) == 0 {
		return nil, errMalformed
	}

	return found, nil
}

func webpChunkSizes(chunks []byte) ([]size, error) {
	var found []size

	for i := 0; i+webpChunkHeader <= len(chunks); {
		fourCC := string(chunks[i : i+4])
		length := int(binary.LittleEndian.Uint32(chunks[i+4 : i+8]))

		end := i + webpChunkHeader + length + length%2
		if length < 0 || end > len(chunks) {
			return nil, errMalformed
		}

		payload := chunks[i+webpChunkHeader : i+webpChunkHeader+length]

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
		case "ANMF":
			if len(payload) < webpFrameHeaderSize {
				return nil, errMalformed
			}

			// the size of the frame comes after its position, and is stored as the size minus one like the canvas
			found = append(found, size{
				width:  1 + (int(payload[6]) | int(payload[7])<<8 | int(payload[8])<<16),
				height: 1 + (int(payload[9]) | int(payload[10])<<8 | int(payload[11])<<16),
			})

			inside, err := webpChunkSizes(payload[webpFrameHeaderSize:])
			if err != nil {
				return nil, err
			}

			found = append(found, inside...)
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

	return found, nil
}
