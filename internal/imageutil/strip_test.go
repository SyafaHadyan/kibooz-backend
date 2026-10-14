package imageutil

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/apperror"
)

const stripMaxBytes = 1 << 20

type byteOrder interface {
	binary.ByteOrder
	binary.AppendByteOrder
}

func testPicture() *image.RGBA {
	picture := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for x := range 16 {
		for y := range 16 {
			picture.Set(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 16), B: 90, A: 255})
		}
	}

	return picture
}

// exifBlock builds the TIFF structure of an EXIF block that names a camera, points to a GPS block and sets an orientation
func exifBlock(order byteOrder, orientation uint16) []byte {
	var tiff bytes.Buffer

	if order == binary.LittleEndian {
		tiff.WriteString("II")
	} else {
		tiff.WriteString("MM")
	}

	tiff.Write(order.AppendUint16(nil, 42))
	tiff.Write(order.AppendUint32(nil, 8))
	tiff.Write(order.AppendUint16(nil, 3))

	camera := []byte("SecretCamera\x00")
	dataAt := uint32(8 + 2 + 3*12 + 4)

	entry := func(tag, kind uint16, count, value uint32) {
		tiff.Write(order.AppendUint16(nil, tag))
		tiff.Write(order.AppendUint16(nil, kind))
		tiff.Write(order.AppendUint32(nil, count))
		tiff.Write(order.AppendUint32(nil, value))
	}

	entry(0x010F, 2, uint32(len(camera)), dataAt)

	tiff.Write(order.AppendUint16(nil, 0x0112))
	tiff.Write(order.AppendUint16(nil, 3))
	tiff.Write(order.AppendUint32(nil, 1))
	tiff.Write(order.AppendUint16(nil, orientation))
	tiff.Write([]byte{0, 0})

	entry(0x8825, 4, 1, 0)
	tiff.Write(order.AppendUint32(nil, 0))
	tiff.Write(camera)

	return tiff.Bytes()
}

func jpegSegment(marker byte, payload []byte) []byte {
	segment := []byte{0xFF, marker}
	segment = binary.BigEndian.AppendUint16(segment, uint16(len(payload)+2))

	return append(segment, payload...)
}

// withJPEGMetadata puts metadata segments and trailing data around the image of an encoded JPEG
func withJPEGMetadata(t *testing.T, encoded []byte, segments ...[]byte) []byte {
	t.Helper()

	require.Equal(t, []byte{0xFF, 0xD8}, encoded[:2])

	out := append([]byte{}, encoded[:2]...)
	for _, segment := range segments {
		out = append(out, segment...)
	}

	out = append(out, encoded[2:]...)

	return append(out, []byte("TRAILING-SECRET")...)
}

func encodeJPEG(t *testing.T) []byte {
	t.Helper()

	var buffer bytes.Buffer
	require.NoError(t, jpeg.Encode(&buffer, testPicture(), &jpeg.Options{Quality: 80}))

	return buffer.Bytes()
}

func pixels(t *testing.T, decode func() (image.Image, error)) []color.Color {
	t.Helper()

	decoded, err := decode()
	require.NoError(t, err)

	var out []color.Color

	bounds := decoded.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			out = append(out, decoded.At(x, y))
		}
	}

	return out
}

func TestStripJPEGRemovesMetadataAndKeepsPixels(t *testing.T) {
	encoded := encodeJPEG(t)

	dirty := withJPEGMetadata(t, encoded,
		jpegSegment(0xE1, append([]byte("Exif\x00\x00"), exifBlock(binary.LittleEndian, 6)...)),
		jpegSegment(0xE1, []byte("http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta>SecretXMP</x:xmpmeta>")),
		jpegSegment(0xED, []byte("Photoshop 3.0\x008BIMSecretIPTC")),
		jpegSegment(0xFE, []byte("SecretComment")),
	)

	clean, err := Inspect(dirty, stripMaxBytes)
	require.NoError(t, err)

	for _, secret := range []string{"SecretCamera", "SecretXMP", "SecretIPTC", "SecretComment", "TRAILING-SECRET"} {
		require.NotContains(t, string(clean.Data), secret)
	}

	require.Equal(t,
		pixels(t, func() (image.Image, error) { return jpeg.Decode(bytes.NewReader(encoded)) }),
		pixels(t, func() (image.Image, error) { return jpeg.Decode(bytes.NewReader(clean.Data)) }),
	)

	// the orientation is the only thing that stays
	require.Contains(t, string(clean.Data), "Exif\x00\x00MM\x00*")
	require.Equal(t, 6, jpegOrientation(t, clean.Data))
}

func jpegOrientation(t *testing.T, data []byte) int {
	t.Helper()

	i := bytes.Index(data, []byte("Exif\x00\x00"))
	require.GreaterOrEqual(t, i, 0)

	return exifOrientation(data[i+6:])
}

func TestStripJPEGKeepsBigEndianAndLittleEndianOrientation(t *testing.T) {
	for _, order := range []byteOrder{binary.LittleEndian, binary.BigEndian} {
		dirty := withJPEGMetadata(t, encodeJPEG(t),
			jpegSegment(0xE1, append([]byte("Exif\x00\x00"), exifBlock(order, 8)...)),
		)

		clean, err := Inspect(dirty, stripMaxBytes)
		require.NoError(t, err)
		require.Equal(t, 8, jpegOrientation(t, clean.Data))
	}
}

func TestStripJPEGWritesNothingForTheDefaultOrientation(t *testing.T) {
	for _, orientation := range []uint16{0, 1, 9} {
		dirty := withJPEGMetadata(t, encodeJPEG(t),
			jpegSegment(0xE1, append([]byte("Exif\x00\x00"), exifBlock(binary.BigEndian, orientation)...)),
		)

		clean, err := Inspect(dirty, stripMaxBytes)
		require.NoError(t, err)
		require.NotContains(t, string(clean.Data), "Exif")
	}
}

func TestStripJPEGKeepsTheColourProfileAndTheJFIFHeader(t *testing.T) {
	encoded := encodeJPEG(t)
	jfif := jpegSegment(0xE0, []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"))
	profile := jpegSegment(0xE2, append([]byte("ICC_PROFILE\x00\x01\x01"), bytes.Repeat([]byte{7}, 40)...))
	mpf := jpegSegment(0xE2, []byte("MPF\x00SecretMPF"))

	clean, err := Inspect(withJPEGMetadata(t, encoded, jfif, profile, mpf), stripMaxBytes)
	require.NoError(t, err)

	require.Contains(t, string(clean.Data), "ICC_PROFILE")
	require.Contains(t, string(clean.Data), "JFIF")
	require.NotContains(t, string(clean.Data), "SecretMPF")
}

func TestStripJPEGDropsAnApplicationSegmentThatIsNotTheJFIFHeader(t *testing.T) {
	encoded := encodeJPEG(t)

	for name, payload := range map[string][]byte{
		"a JFXX extension with a thumbnail": []byte("JFXX\x00\x10SecretThumbnail"),
		"another use of the segment":        []byte("Ducky\x00SecretDucky"),
		"an empty segment":                  {},
	} {
		clean, err := Inspect(withJPEGMetadata(t, encoded, jpegSegment(0xE0, payload)), stripMaxBytes)
		require.NoError(t, err, name)

		require.NotContains(t, string(clean.Data), "Secret", name)
		require.NotContains(t, string(clean.Data), "JFXX", name)
	}
}

func TestStripJPEGKeepsOnlyTheFirstJFIFHeaderWithoutItsTail(t *testing.T) {
	jfif := func(tail string) []byte {
		return jpegSegment(0xE0, []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"+tail))
	}

	clean, err := Inspect(withJPEGMetadata(t, encodeJPEG(t), jfif("first"), jfif("second")), stripMaxBytes)
	require.NoError(t, err)

	require.Equal(t, 1, bytes.Count(clean.Data, []byte("JFIF")))
	require.NotContains(t, string(clean.Data), "first")
	require.NotContains(t, string(clean.Data), "second")
}

func TestStripJPEGRemovesTheThumbnailOfTheJFIFHeader(t *testing.T) {
	// the header says that a thumbnail of 2 by 2 pixels follows, and its 12 bytes could hold anything
	header := []byte("JFIF\x00\x01\x02\x01\x00\x48\x00\x48\x02\x02SecretThumb")

	clean, err := Inspect(withJPEGMetadata(t, encodeJPEG(t), jpegSegment(0xE0, header)), stripMaxBytes)
	require.NoError(t, err)

	require.NotContains(t, string(clean.Data), "SecretThumb")

	want := jpegSegment(0xE0, []byte("JFIF\x00\x01\x02\x01\x00\x48\x00\x48\x00\x00"))
	require.Equal(t, want, clean.Data[2:2+len(want)], "the version, units and density stay and the thumbnail is 0 by 0")
}

func TestStripJPEGDropsAJFIFHeaderThatIsTooShort(t *testing.T) {
	clean, err := Inspect(withJPEGMetadata(t, encodeJPEG(t), jpegSegment(0xE0, []byte("JFIF\x00\x01\x01"))), stripMaxBytes)
	require.NoError(t, err)
	require.NotContains(t, string(clean.Data), "JFIF")
}

func TestStripJPEGCutsTheAdobeSegmentToItsFields(t *testing.T) {
	adobe := jpegSegment(0xEE, []byte("Adobe\x00\x64\x00\x00\x00\x00\x01SecretAdobe"))

	clean, err := Inspect(withJPEGMetadata(t, encodeJPEG(t), adobe), stripMaxBytes)
	require.NoError(t, err)

	require.NotContains(t, string(clean.Data), "SecretAdobe")
	require.Contains(t, string(clean.Data), string(jpegSegment(0xEE, []byte("Adobe\x00\x64\x00\x00\x00\x00\x01"))))

	short, err := Inspect(withJPEGMetadata(t, encodeJPEG(t), jpegSegment(0xEE, []byte("Adobe\x00"))), stripMaxBytes)
	require.NoError(t, err)
	require.NotContains(t, string(short.Data), "Adobe")
}

func TestStripJPEGDropsContentCredentialsAndMarkersItDoesNotKnow(t *testing.T) {
	clean, err := Inspect(withJPEGMetadata(t, encodeJPEG(t),
		jpegSegment(0xEB, []byte("JP\x00\x00jumbc2paSecretC2PA")),
		jpegSegment(0x02, []byte("SecretReserved")),
		jpegSegment(0xC8, []byte("SecretJPG")),
		jpegSegment(0xDE, []byte("SecretHierarchical")),
		jpegSegment(0xF3, []byte("SecretExtension")),
	), stripMaxBytes)
	require.NoError(t, err)

	require.NotContains(t, string(clean.Data), "Secret")

	_, err = jpeg.Decode(bytes.NewReader(clean.Data))
	require.NoError(t, err)
}

func TestStripJPEGIsAFixedPoint(t *testing.T) {
	dirty := withJPEGMetadata(t, encodeJPEG(t),
		jpegSegment(0xE1, append([]byte("Exif\x00\x00"), exifBlock(binary.LittleEndian, 3)...)),
	)

	once, err := Inspect(dirty, stripMaxBytes)
	require.NoError(t, err)

	twice, err := Inspect(once.Data, stripMaxBytes)
	require.NoError(t, err)
	require.Equal(t, once.Data, twice.Data)
}

func TestStripJPEGClosesAFileThatEndsInsideTheScan(t *testing.T) {
	encoded := encodeJPEG(t)
	truncated := encoded[:len(encoded)-2]

	clean, err := Inspect(truncated, stripMaxBytes)
	require.NoError(t, err)
	require.Equal(t, []byte{0xFF, 0xD9}, clean.Data[len(clean.Data)-2:])

	_, err = jpeg.Decode(bytes.NewReader(clean.Data))
	require.NoError(t, err)
}

func TestStripRejectsAStructureThatDoesNotParse(t *testing.T) {
	encoded := encodeJPEG(t)

	for name, data := range map[string][]byte{
		"jpeg with only a signature":                 {0xFF, 0xD8, 0xFF},
		"jpeg with a segment that runs past the end": append(append([]byte{}, encoded[:2]...), 0xFF, 0xE1, 0xFF, 0xFF, 1, 2, 3),
		"jpeg with a length below two":               append(append([]byte{}, encoded[:2]...), 0xFF, 0xE1, 0x00, 0x01, 0x00),
		"png with only a signature":                  append([]byte{}, pngSignature...),
		"png without an end":                         encodePNG(t)[:40],
		"webp with a chunk past the end":             []byte("RIFF\x10\x00\x00\x00WEBPVP8L\xff\x00\x00\x00"),
		"webp without chunks":                        []byte("RIFF\x04\x00\x00\x00WEBP"),
	} {
		_, err := Inspect(data, stripMaxBytes)
		require.ErrorIs(t, err, apperror.ErrInvalidImage, name)
	}
}

func encodePNG(t *testing.T) []byte {
	t.Helper()

	var buffer bytes.Buffer
	require.NoError(t, png.Encode(&buffer, testPicture()))

	return buffer.Bytes()
}

func pngChunk(kind string, payload []byte) []byte {
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(payload)))
	chunk = append(chunk, kind...)
	chunk = append(chunk, payload...)

	return binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(append([]byte(kind), payload...)))
}

func pngChunkTypes(t *testing.T, data []byte) []string {
	t.Helper()

	var types []string

	for i := len(pngSignature); i+8 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[i : i+4]))
		types = append(types, string(data[i+4:i+8]))
		i += 12 + length
	}

	return types
}

func TestStripPNGRemovesMetadataChunksAndKeepsPixels(t *testing.T) {
	encoded := encodePNG(t)

	// the first chunk is the header, which is 25 bytes with the signature in front of it
	headerEnd := len(pngSignature) + 25

	var dirty []byte
	dirty = append(dirty, encoded[:headerEnd]...)
	dirty = append(dirty, pngChunk("tEXt", []byte("Comment\x00SecretText"))...)
	dirty = append(dirty, pngChunk("eXIf", exifBlock(binary.BigEndian, 6))...)
	dirty = append(dirty, pngChunk("iTXt", []byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00SecretXMP"))...)
	dirty = append(dirty, pngChunk("tIME", []byte{7, 234, 10, 10, 1, 2, 3})...)
	dirty = append(dirty, pngChunk("gAMA", []byte{0, 1, 134, 160})...)
	dirty = append(dirty, encoded[headerEnd:]...)
	dirty = append(dirty, []byte("TRAILING-SECRET")...)

	clean, err := Inspect(dirty, stripMaxBytes)
	require.NoError(t, err)

	for _, secret := range []string{"SecretText", "SecretXMP", "SecretCamera", "TRAILING-SECRET"} {
		require.NotContains(t, string(clean.Data), secret)
	}

	types := pngChunkTypes(t, clean.Data)
	require.Equal(t, "IHDR", types[0])
	require.Equal(t, "IEND", types[len(types)-1])
	require.Contains(t, types, "gAMA")
	require.NotContains(t, types, "tEXt")
	require.NotContains(t, types, "eXIf")
	require.NotContains(t, types, "iTXt")
	require.NotContains(t, types, "tIME")

	require.Equal(t,
		pixels(t, func() (image.Image, error) { return png.Decode(bytes.NewReader(encoded)) }),
		pixels(t, func() (image.Image, error) { return png.Decode(bytes.NewReader(clean.Data)) }),
	)
}

func webpChunk(fourCC string, payload []byte) []byte {
	chunk := append([]byte(fourCC), binary.LittleEndian.AppendUint32(nil, uint32(len(payload)))...)
	chunk = append(chunk, payload...)

	if len(payload)%2 == 1 {
		chunk = append(chunk, 0)
	}

	return chunk
}

func webpFile(chunks ...[]byte) []byte {
	body := []byte("WEBP")
	for _, chunk := range chunks {
		body = append(body, chunk...)
	}

	file := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...)

	return append(file, body...)
}

func webpChunks(t *testing.T, data []byte) map[string][]byte {
	t.Helper()

	require.Equal(t, "RIFF", string(data[0:4]))
	require.Equal(t, len(data)-8, int(binary.LittleEndian.Uint32(data[4:8])))

	chunks := map[string][]byte{}

	for i := 12; i < len(data); {
		size := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))
		chunks[string(data[i:i+4])] = data[i+8 : i+8+size]
		i += 8 + size + size%2
	}

	return chunks
}

func TestStripWebPRemovesExifAndXMPAndKeepsTheOrientation(t *testing.T) {
	// the flags say that the file has alpha, EXIF and XMP
	flags := []byte{0x10 | webpFlagsEXIF | webpFlagsXMP, 0, 0, 0, 15, 0, 0, 15, 0, 0}

	dirty := webpFile(
		webpChunk("VP8X", flags),
		webpChunk("ICCP", []byte("profile")),
		webpChunk("VP8L", vp8l(16, 16)),
		webpChunk("EXIF", exifBlock(binary.LittleEndian, 3)),
		webpChunk("XMP ", []byte("SecretXMP")),
	)

	clean, err := Inspect(dirty, stripMaxBytes)
	require.NoError(t, err)

	require.NotContains(t, string(clean.Data), "SecretCamera")
	require.NotContains(t, string(clean.Data), "SecretXMP")

	chunks := webpChunks(t, clean.Data)
	require.Equal(t, byte(0x10|webpFlagsEXIF), chunks["VP8X"][0])
	require.Equal(t, vp8l(16, 16), chunks["VP8L"])
	require.Equal(t, []byte("profile"), chunks["ICCP"])
	require.NotContains(t, chunks, "XMP ")
	require.Equal(t, 3, exifOrientation(chunks["EXIF"]))
	require.Len(t, chunks["EXIF"], 26)
}

func TestStripWebPClearsTheFlagsWhenNoOrientationStays(t *testing.T) {
	flags := []byte{webpFlagsEXIF | webpFlagsXMP, 0, 0, 0, 15, 0, 0, 15, 0, 0}

	clean, err := Inspect(webpFile(
		webpChunk("VP8X", flags),
		webpChunk("VP8L", vp8l(16, 16)),
		webpChunk("EXIF", exifBlock(binary.BigEndian, 1)),
		webpChunk("XMP ", []byte("SecretXMP")),
	), stripMaxBytes)
	require.NoError(t, err)

	chunks := webpChunks(t, clean.Data)
	require.Equal(t, byte(0), chunks["VP8X"][0])
	require.NotContains(t, chunks, "EXIF")
	require.NotContains(t, chunks, "XMP ")
}

func TestStripWebPWithoutTheExtendedHeaderCannotCarryAnOrientation(t *testing.T) {
	clean, err := Inspect(webpFile(
		webpChunk("VP8L", vp8l(16, 16)),
		webpChunk("EXIF", exifBlock(binary.BigEndian, 6)),
	), stripMaxBytes)
	require.NoError(t, err)

	chunks := webpChunks(t, clean.Data)
	require.Equal(t, vp8l(16, 16), chunks["VP8L"])
	require.NotContains(t, chunks, "EXIF")
}

func TestExifOrientationIgnoresDamagedBlocks(t *testing.T) {
	valid := exifBlock(binary.BigEndian, 5)
	require.Equal(t, 5, exifOrientation(valid))

	for name, tiff := range map[string][]byte{
		"empty":                nil,
		"short":                valid[:6],
		"wrong byte order":     append([]byte("XX"), valid[2:]...),
		"wrong magic number":   append([]byte("MM\x00\x2b"), valid[4:]...),
		"directory past end":   append(append([]byte{}, valid[:4]...), 0xFF, 0xFF, 0xFF, 0xFF),
		"entries past the end": valid[:20],
	} {
		require.Zero(t, exifOrientation(tiff), name)
	}
}

func TestStripPNGKeepsOnlyTheChunksThatTheImageNeeds(t *testing.T) {
	encoded := encodePNG(t)
	headerEnd := len(pngSignature) + 25

	var dirty []byte
	dirty = append(dirty, encoded[:headerEnd]...)

	for _, kind := range []string{"gAMA", "cHRM", "sRGB", "iCCP", "sBIT", "cICP", "bKGD", "pHYs", "tRNS"} {
		dirty = append(dirty, pngChunk(kind, []byte{0, 0, 0, 1, 0, 0, 0, 1})...)
	}

	// content credentials, a private chunk, a suggested palette, a stereo marker and a chunk that does not exist yet
	for _, kind := range []string{"caBX", "prVt", "sPLT", "sTER", "xyZZ", "oFFs", "pCAL"} {
		dirty = append(dirty, pngChunk(kind, []byte("SecretChunk"))...)
	}

	dirty = append(dirty, encoded[headerEnd:]...)

	clean, err := Inspect(dirty, stripMaxBytes)
	require.NoError(t, err)
	require.NotContains(t, string(clean.Data), "SecretChunk")

	types := pngChunkTypes(t, clean.Data)
	for _, kind := range []string{"gAMA", "cHRM", "sRGB", "iCCP", "sBIT", "cICP", "bKGD", "pHYs", "tRNS"} {
		require.Contains(t, types, kind)
	}

	for _, kind := range []string{"caBX", "prVt", "sPLT", "sTER", "xyZZ", "oFFs", "pCAL"} {
		require.NotContains(t, types, kind)
	}

	require.Equal(t, "IHDR", types[0])
	require.Contains(t, types, "IDAT")
	require.Equal(t, "IEND", types[len(types)-1])
}

func TestStripPNGRefusesACriticalChunkItDoesNotKnow(t *testing.T) {
	encoded := encodePNG(t)
	headerEnd := len(pngSignature) + 25

	var dirty []byte
	dirty = append(dirty, encoded[:headerEnd]...)
	dirty = append(dirty, pngChunk("XTRA", []byte("a chunk that decides how the image reads"))...)
	dirty = append(dirty, encoded[headerEnd:]...)

	_, err := Inspect(dirty, stripMaxBytes)
	require.ErrorIs(t, err, apperror.ErrInvalidImage)

	// the same chunk with a lower case first letter is optional, so it is only dropped
	dirty = append([]byte{}, encoded[:headerEnd]...)
	dirty = append(dirty, pngChunk("xTRA", []byte("optional"))...)
	dirty = append(dirty, encoded[headerEnd:]...)

	clean, err := Inspect(dirty, stripMaxBytes)
	require.NoError(t, err)
	require.NotContains(t, pngChunkTypes(t, clean.Data), "xTRA")
}

func TestStripWebPKeepsOnlyTheChunksThatTheImageNeeds(t *testing.T) {
	flags := []byte{0x10, 0, 0, 0, 15, 0, 0, 15, 0, 0}

	clean, err := Inspect(webpFile(
		webpChunk("VP8X", flags),
		webpChunk("ICCP", []byte("profile")),
		webpChunk("C2PA", []byte("SecretC2PA")),
		webpChunk("ALPH", []byte("alpha")),
		webpChunk("VP8L", vp8l(16, 16)),
		webpChunk("xyZZ", []byte("SecretUnknown")),
	), stripMaxBytes)
	require.NoError(t, err)

	require.NotContains(t, string(clean.Data), "Secret")

	chunks := webpChunks(t, clean.Data)
	require.Equal(t, []byte("profile"), chunks["ICCP"])
	require.Equal(t, []byte("alpha"), chunks["ALPH"])
	require.Contains(t, chunks, "VP8X")
	require.Contains(t, chunks, "VP8L")
	require.NotContains(t, chunks, "C2PA")
	require.NotContains(t, chunks, "xyZZ")
}

func TestStripWebPCleansTheChunksInsideTheFramesOfAnAnimation(t *testing.T) {
	frame := func(chunks ...[]byte) []byte {
		payload := make([]byte, 16)
		for _, chunk := range chunks {
			payload = append(payload, chunk...)
		}

		return payload
	}

	flags := []byte{0x02, 0, 0, 0, 15, 0, 0, 15, 0, 0}

	clean, err := Inspect(webpFile(
		webpChunk("VP8X", flags),
		webpChunk("ANIM", []byte{0, 0, 0, 0, 0, 0}),
		webpChunk("ANMF", frame(webpChunk("VP8L", vp8l(16, 16)), webpChunk("xyZZ", []byte("SecretFrame")))),
		webpChunk("ANMF", frame(webpChunk("ALPH", []byte("a")), webpChunk("VP8 ", vp8(16, 16)), webpChunk("EXIF", []byte("SecretFrameExif")))),
	), stripMaxBytes)
	require.NoError(t, err)

	require.NotContains(t, string(clean.Data), "Secret")
	require.Equal(t, 2, bytes.Count(clean.Data, []byte("ANMF")))
	require.Contains(t, string(clean.Data), "ANIM")

	// the sizes of the file and of the frames were written again, so the structure still parses
	_, err = Inspect(clean.Data, stripMaxBytes)
	require.NoError(t, err)
}

func TestStripWebPRefusesAFrameWithoutAnImage(t *testing.T) {
	flags := []byte{0x02, 0, 0, 0, 15, 0, 0, 15, 0, 0}

	for name, payload := range map[string][]byte{
		"a frame shorter than its header":   make([]byte, 4),
		"a frame with only unknown chunks":  append(make([]byte, 16), webpChunk("xyZZ", []byte("x"))...),
		"a chunk past the end of the frame": append(make([]byte, 16), []byte("VP8L\xff\x00\x00\x00")...),
	} {
		_, err := Inspect(webpFile(webpChunk("VP8X", flags), webpChunk("ANMF", payload)), stripMaxBytes)
		require.ErrorIs(t, err, apperror.ErrInvalidImage, name)
	}
}
