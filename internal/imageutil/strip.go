package imageutil

import (
	"bytes"
	"encoding/binary"
	"errors"
)

var errMalformed = errors.New("malformed image")

// strip removes the metadata of an image without decoding it, so the pixels stay exactly as they were. Location, camera,
// time and software details live in the metadata, so none of them is stored. Only the orientation of an EXIF block is
// kept, because a photo from a phone would show up sideways without it.
func strip(contentType string, data []byte) ([]byte, error) {
	switch contentType {
	case "image/jpeg":
		return stripJPEG(data)
	case "image/png":
		return stripPNG(data)
	case "image/webp":
		return stripWebP(data)
	default:
		return nil, errMalformed
	}
}

const (
	jpegMarkerPrefix = 0xFF
	jpegSOI          = 0xD8
	jpegEOI          = 0xD9
	jpegSOS          = 0xDA
	jpegAPP0         = 0xE0
	jpegAPP1         = 0xE1
	jpegAPP2         = 0xE2
	jpegAPP14        = 0xEE
	jpegTEM          = 0x01
	jpegRST0         = 0xD0
	jpegRST7         = 0xD7
)

func stripJPEG(data []byte) ([]byte, error) {
	if len(data) < 4 || data[0] != jpegMarkerPrefix || data[1] != jpegSOI {
		return nil, errMalformed
	}

	var (
		app0        []byte
		rest        bytes.Buffer
		orientation int
		i           = 2
	)

	for {
		// markers may be preceded by any number of 0xFF fill bytes
		for i < len(data) && data[i] == jpegMarkerPrefix && i+1 < len(data) && data[i+1] == jpegMarkerPrefix {
			i++
		}

		if i+1 >= len(data) || data[i] != jpegMarkerPrefix {
			return nil, errMalformed
		}

		marker := data[i+1]
		i += 2

		switch {
		case marker == jpegEOI:
			rest.Write([]byte{jpegMarkerPrefix, jpegEOI})
			return assembleJPEG(app0, orientation, rest.Bytes()), nil
		case marker == jpegSOI || marker == 0x00:
			return nil, errMalformed
		case marker == jpegTEM || (marker >= jpegRST0 && marker <= jpegRST7):
			rest.Write([]byte{jpegMarkerPrefix, marker})
			continue
		}

		if i+2 > len(data) {
			return nil, errMalformed
		}

		length := int(binary.BigEndian.Uint16(data[i : i+2]))
		if length < 2 || i+length > len(data) {
			return nil, errMalformed
		}

		payload := data[i+2 : i+length]
		segment := data[i-2 : i+length]
		i += length

		switch {
		case marker == jpegAPP0:
			if app0 == nil {
				app0 = segment
			}
		case marker == jpegAPP1:
			if bytes.HasPrefix(payload, []byte("Exif\x00\x00")) && orientation == 0 {
				orientation = exifOrientation(payload[6:])
			}
		case marker == jpegAPP2:
			if bytes.HasPrefix(payload, []byte("ICC_PROFILE\x00")) {
				rest.Write(segment)
			}
		case marker == jpegAPP14:
			if bytes.HasPrefix(payload, []byte("Adobe")) {
				rest.Write(segment)
			}
		case marker > jpegAPP0 && marker <= 0xEF, marker == 0xFE:
			// the other application segments and the comments hold metadata only
		default:
			rest.Write(segment)
		}

		if marker != jpegSOS {
			continue
		}

		// the compressed scan follows the header of the scan and runs until the next marker that is not part of it
		start := i
		for i < len(data) {
			if data[i] != jpegMarkerPrefix {
				i++
				continue
			}

			if i+1 >= len(data) {
				i++
				continue
			}

			next := data[i+1]
			if next == 0x00 || (next >= jpegRST0 && next <= jpegRST7) {
				i += 2
				continue
			}

			if next == jpegMarkerPrefix {
				i++
				continue
			}

			break
		}

		rest.Write(data[start:i])

		// a file that ends inside the scan is still shown by every decoder, so close it properly
		if i >= len(data) {
			rest.Write([]byte{jpegMarkerPrefix, jpegEOI})
			return assembleJPEG(app0, orientation, rest.Bytes()), nil
		}
	}
}

func assembleJPEG(app0 []byte, orientation int, rest []byte) []byte {
	out := make([]byte, 0, 2+len(app0)+36+len(rest))
	out = append(out, jpegMarkerPrefix, jpegSOI)
	out = append(out, app0...)

	if orientation > 1 {
		tiff := orientationTIFF(orientation)
		out = append(out, jpegMarkerPrefix, jpegAPP1)
		out = binary.BigEndian.AppendUint16(out, uint16(2+6+len(tiff)))
		out = append(out, "Exif\x00\x00"...)
		out = append(out, tiff...)
	}

	return append(out, rest...)
}

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// the chunks of a PNG that carry text, time or EXIF details
var pngMetadataChunks = map[string]bool{
	"eXIf": true,
	"tEXt": true,
	"zTXt": true,
	"iTXt": true,
	"tIME": true,
}

func stripPNG(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, pngSignature) {
		return nil, errMalformed
	}

	out := make([]byte, 0, len(data))
	out = append(out, pngSignature...)
	i := len(pngSignature)

	for {
		if i+8 > len(data) {
			return nil, errMalformed
		}

		length := int(binary.BigEndian.Uint32(data[i : i+4]))
		chunkType := string(data[i+4 : i+8])

		end := i + 8 + length + 4
		if length < 0 || length > len(data) || end > len(data) {
			return nil, errMalformed
		}

		if !pngMetadataChunks[chunkType] {
			out = append(out, data[i:end]...)
		}

		i = end

		if chunkType == "IEND" {
			return out, nil
		}
	}
}

const (
	webpHeaderSize  = 12
	webpFlagsEXIF   = 0x08
	webpFlagsXMP    = 0x04
	webpChunkHeader = 8
)

func stripWebP(data []byte) ([]byte, error) {
	if len(data) < webpHeaderSize || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil, errMalformed
	}

	riffSize := int(binary.LittleEndian.Uint32(data[4:8]))
	if riffSize < 4 || riffSize+8 > len(data) {
		return nil, errMalformed
	}

	data = data[:riffSize+8]

	var (
		chunks      bytes.Buffer
		orientation int
		extended    bool
		flagsAt     = -1
		i           = webpHeaderSize
	)

	for i < len(data) {
		if i+webpChunkHeader > len(data) {
			return nil, errMalformed
		}

		fourCC := string(data[i : i+4])
		size := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))

		// every chunk is padded to an even size
		end := i + webpChunkHeader + size + size%2
		if size < 0 || size > len(data) || end > len(data) {
			return nil, errMalformed
		}

		payload := data[i+webpChunkHeader : i+webpChunkHeader+size]

		switch fourCC {
		case "EXIF":
			if orientation == 0 {
				orientation = exifOrientation(bytes.TrimPrefix(payload, []byte("Exif\x00\x00")))
			}
		case "XMP ":
		default:
			if fourCC == "VP8X" && size >= 10 {
				extended = true
				flagsAt = chunks.Len() + webpChunkHeader
			}

			chunks.Write(data[i:end])
		}

		i = end
	}

	body := chunks.Bytes()
	if len(body) == 0 {
		return nil, errMalformed
	}

	keepOrientation := orientation > 1 && extended

	if flagsAt >= 0 {
		body[flagsAt] &^= webpFlagsEXIF | webpFlagsXMP
		if keepOrientation {
			body[flagsAt] |= webpFlagsEXIF
		}
	}

	if keepOrientation {
		tiff := orientationTIFF(orientation)
		body = append(body, "EXIF"...)
		body = binary.LittleEndian.AppendUint32(body, uint32(len(tiff)))
		body = append(body, tiff...)
	}

	out := make([]byte, 0, webpHeaderSize+len(body))
	out = append(out, "RIFF"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(4+len(body)))
	out = append(out, "WEBP"...)

	return append(out, body...), nil
}

const exifOrientationTag = 0x0112

// exifOrientation reads the orientation from the TIFF structure of an EXIF block and returns 0 when there is none that
// makes sense
func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 0
	}

	var order binary.ByteOrder

	switch string(tiff[0:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}

	if order.Uint16(tiff[2:4]) != 42 {
		return 0
	}

	ifd := int(order.Uint32(tiff[4:8]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return 0
	}

	count := int(order.Uint16(tiff[ifd : ifd+2]))

	for n := range count {
		entry := ifd + 2 + n*12
		if entry+12 > len(tiff) {
			return 0
		}

		if order.Uint16(tiff[entry:entry+2]) != exifOrientationTag {
			continue
		}

		// the type is SHORT and the value sits inside the entry
		if order.Uint16(tiff[entry+2:entry+4]) != 3 || order.Uint32(tiff[entry+4:entry+8]) != 1 {
			return 0
		}

		value := int(order.Uint16(tiff[entry+8 : entry+10]))
		if value < 1 || value > 8 {
			return 0
		}

		return value
	}

	return 0
}

// orientationTIFF builds the smallest TIFF structure that holds an orientation and nothing else
func orientationTIFF(orientation int) []byte {
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8}
	tiff = binary.BigEndian.AppendUint16(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, exifOrientationTag)
	tiff = binary.BigEndian.AppendUint16(tiff, 3)
	tiff = binary.BigEndian.AppendUint32(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, uint16(orientation))
	tiff = append(tiff, 0, 0)

	// no further image directory
	return binary.BigEndian.AppendUint32(tiff, 0)
}
