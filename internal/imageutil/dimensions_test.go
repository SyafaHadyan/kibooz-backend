package imageutil

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
)

// vp8l is the start of a lossless WebP image of the given size, which is all that is read from it
func vp8l(width int, height int) []byte {
	bits := uint32(width-1) | uint32(height-1)<<14

	return append([]byte{0x2f}, binary.LittleEndian.AppendUint32(nil, bits)...)
}

// vp8 is the start of a lossy WebP frame of the given size
func vp8(width int, height int) []byte {
	frame := []byte{0, 0, 0, 0x9d, 0x01, 0x2a}
	frame = binary.LittleEndian.AppendUint16(frame, uint16(width))

	return binary.LittleEndian.AppendUint16(frame, uint16(height))
}

// vp8x is the extended header with a canvas of the given size
func vp8x(width int, height int) []byte {
	header := []byte{0, 0, 0, 0}
	header = append(header, byte(width-1), byte((width-1)>>8), byte((width-1)>>16))

	return append(header, byte(height-1), byte((height-1)>>8), byte((height-1)>>16))
}

// pngOfSize builds a PNG whose header announces the size and that has no pixels, which is enough to read the size
func pngOfSize(width int, height int) []byte {
	header := binary.BigEndian.AppendUint32(nil, uint32(width))
	header = binary.BigEndian.AppendUint32(header, uint32(height))
	header = append(header, 8, 2, 0, 0, 0)

	out := append([]byte{}, pngSignature...)
	out = append(out, checkedPNGChunk("IHDR", header)...)
	out = append(out, checkedPNGChunk("IDAT", nil)...)

	return append(out, checkedPNGChunk("IEND", nil)...)
}

func checkedPNGChunk(kind string, payload []byte) []byte {
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(payload)))
	chunk = append(chunk, kind...)
	chunk = append(chunk, payload...)

	return binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(append([]byte(kind), payload...)))
}

// jpegOfSize builds a JPEG with a frame header that announces the size and a scan that holds nothing
func jpegOfSize(width int, height int) []byte {
	frame := []byte{8}
	frame = binary.BigEndian.AppendUint16(frame, uint16(height))
	frame = binary.BigEndian.AppendUint16(frame, uint16(width))
	frame = append(frame, 1, 1, 0x11, 0)

	out := []byte{0xFF, jpegSOI}
	out = append(out, jpegSegment(0xC0, frame)...)
	out = append(out, jpegSegment(jpegSOS, []byte{1, 1, 0, 0, 63, 0})...)

	return append(out, 0xFF, jpegEOI)
}

func TestAnImageWithinTheLimitsIsAccepted(t *testing.T) {
	for name, data := range map[string][]byte{
		"png at the longest side":      pngOfSize(MaxSide, 1),
		"png at the most pixels":       pngOfSize(8000, 5000),
		"jpeg of a phone":              jpegOfSize(4000, 3000),
		"jpeg at the longest side":     jpegOfSize(1, MaxSide),
		"lossy webp":                   webpFile(webpChunk("VP8 ", vp8(1024, 768))),
		"lossless webp":                webpFile(webpChunk("VP8L", vp8l(MaxSide, 1))),
		"extended webp with its image": webpFile(webpChunk("VP8X", vp8x(1000, 1000)), webpChunk("VP8L", vp8l(1000, 1000))),
	} {
		_, err := Inspect(data, stripMaxBytes)
		require.NoError(t, err, name)
	}
}

func TestAnImageBeyondTheLimitsIsRefusedByItsHeader(t *testing.T) {
	for name, data := range map[string][]byte{
		"png too wide":                   pngOfSize(MaxSide+1, 1),
		"png too tall":                   pngOfSize(1, MaxSide+1),
		"png with too many pixels":       pngOfSize(8192, 8192),
		"png of billions of pixels":      pngOfSize(100000, 100000),
		"jpeg too wide":                  jpegOfSize(MaxSide+1, 10),
		"jpeg with too many pixels":      jpegOfSize(8000, 6000),
		"lossy webp too tall":            webpFile(webpChunk("VP8 ", vp8(10, 10000))),
		"lossless webp with too many":    webpFile(webpChunk("VP8L", vp8l(8000, 6000))),
		"webp canvas past the limit":     webpFile(webpChunk("VP8X", vp8x(20000, 20000)), webpChunk("VP8L", vp8l(10, 10))),
		"webp image past a small canvas": webpFile(webpChunk("VP8X", vp8x(10, 10)), webpChunk("VP8L", vp8l(10000, 10))),
	} {
		_, err := Inspect(data, stripMaxBytes)
		require.ErrorIs(t, err, apperror.ErrImageDimensions, name)
	}
}

func TestAnImageWhoseSizeCannotBeReadIsInvalid(t *testing.T) {
	for name, data := range map[string][]byte{
		"png whose size overflows":  pngOfSize(1<<30, 1<<30),
		"png of zero size":          pngOfSize(0, 5),
		"webp without an image":     webpFile(webpChunk("ICCP", []byte("profile"))),
		"lossy webp without start":  webpFile(webpChunk("VP8 ", []byte("no start code here"))),
		"lossless without signal":   webpFile(webpChunk("VP8L", []byte("xxxxxxxx"))),
		"extended header too short": webpFile(webpChunk("VP8X", []byte{0, 0, 0, 0, 1})),
	} {
		_, err := Inspect(data, stripMaxBytes)
		require.ErrorIs(t, err, apperror.ErrInvalidImage, name)
	}
}

func TestTheSizeIsReadFromWhatIsStoredAndNotFromWhatWasSent(t *testing.T) {
	// the stripped file is the one that the apps download, so a size that only the removed bytes announced does not count
	small := pngOfSize(10, 10)
	huge := pngOfSize(MaxSide+1, 10)

	sent := append(bytes.Clone(small), huge...)

	clean, err := Inspect(sent, stripMaxBytes)
	require.NoError(t, err)
	require.Equal(t, small, clean.Data)
}

func TestTheLimitMessageNamesTheLimits(t *testing.T) {
	require.Contains(t, apperror.ErrImageDimensions.Message, "8192")
	require.Contains(t, apperror.ErrImageDimensions.Message, "40 megapixels")
	require.Equal(t, 8192, MaxSide)
	require.Equal(t, 40_000_000, MaxPixels)
}

// anmf is the frame of an animation that announces the given size and holds the chunks
func anmf(width int, height int, chunks ...[]byte) []byte {
	frame := make([]byte, webpFrameHeaderSize)
	frame[6], frame[7], frame[8] = byte(width-1), byte((width-1)>>8), byte((width-1)>>16)
	frame[9], frame[10], frame[11] = byte(height-1), byte((height-1)>>8), byte((height-1)>>16)

	for _, chunk := range chunks {
		frame = append(frame, chunk...)
	}

	return frame
}

func TestEveryFrameOfAnAnimationIsHeldToTheLimits(t *testing.T) {
	canvas := webpChunk("VP8X", vp8x(100, 100))

	t.Run("an animation within the limits is accepted", func(t *testing.T) {
		_, err := Inspect(webpFile(canvas,
			webpChunk("ANMF", anmf(100, 100, webpChunk("VP8L", vp8l(100, 100)))),
			webpChunk("ANMF", anmf(50, 50, webpChunk("VP8 ", vp8(50, 50)))),
		), stripMaxBytes)
		require.NoError(t, err)
	})

	for name, frame := range map[string][]byte{
		"a frame that announces more than the canvas": anmf(20000, 20000, webpChunk("VP8L", vp8l(10, 10))),
		"a frame whose image is wider than allowed":   anmf(10, 10, webpChunk("VP8L", vp8l(10000, 10))),
		"a frame whose lossy image has too many":      anmf(10, 10, webpChunk("VP8 ", vp8(8000, 6000))),
	} {
		_, err := Inspect(webpFile(canvas, webpChunk("ANMF", frame)), stripMaxBytes)
		require.ErrorIs(t, err, apperror.ErrImageDimensions, name)
	}

	t.Run("a frame that is too short to read is invalid", func(t *testing.T) {
		_, err := Inspect(webpFile(canvas, webpChunk("ANMF", make([]byte, 4))), stripMaxBytes)
		require.ErrorIs(t, err, apperror.ErrInvalidImage)
	})

	t.Run("a chunk inside a frame that runs past its end is invalid", func(t *testing.T) {
		_, err := Inspect(webpFile(canvas, webpChunk("ANMF", append(make([]byte, webpFrameHeaderSize), "VP8L\xff\x00\x00\x00"...))), stripMaxBytes)
		require.ErrorIs(t, err, apperror.ErrInvalidImage)
	})
}
