package jobs

import (
	"bytes"
	"encoding/binary"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
)

// creativeFabricaImageDimensions returns dimensions for image guidance before
// the Media Matrix request is built.  The protobuf marks width/height as
// optional, so an unknown or malformed image remains valid and simply omits
// those fields instead of inventing metadata.
func creativeFabricaImageDimensions(raw []byte, mediaType, filename string) (int, int) {
	if len(raw) == 0 {
		return 0, 0
	}
	if config, _, err := image.DecodeConfig(bytes.NewReader(raw)); err == nil && config.Width > 0 && config.Height > 0 {
		return config.Width, config.Height
	}
	if strings.EqualFold(mediaType, "image/webp") || strings.EqualFold(strings.TrimSpace(filenameExtension(filename)), ".webp") {
		return creativeFabricaWebPDimensions(raw)
	}
	return 0, 0
}

func filenameExtension(filename string) string {
	index := strings.LastIndexByte(filename, '.')
	if index < 0 {
		return ""
	}
	return filename[index:]
}

// creativeFabricaWebPDimensions handles the three WebP bitstream containers
// used by browsers without pulling a second image decoder into the gateway.
func creativeFabricaWebPDimensions(raw []byte) (int, int) {
	if len(raw) < 16 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WEBP" {
		return 0, 0
	}
	for offset := 12; offset+8 <= len(raw); {
		chunkType := string(raw[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(raw[offset+4 : offset+8]))
		payload := offset + 8
		if payload > len(raw) || chunkSize < 0 || chunkSize > len(raw)-payload {
			return 0, 0
		}
		chunk := raw[payload : payload+chunkSize]
		switch chunkType {
		case "VP8X":
			if len(chunk) >= 10 {
				width := 1 + (int(chunk[4]) | int(chunk[5])<<8 | int(chunk[6])<<16)
				height := 1 + (int(chunk[7]) | int(chunk[8])<<8 | int(chunk[9])<<16)
				return width, height
			}
		case "VP8L":
			if len(chunk) >= 5 && chunk[0] == 0x2f {
				bits := uint32(chunk[1]) | uint32(chunk[2])<<8 | uint32(chunk[3])<<16 | uint32(chunk[4])<<24
				width := 1 + int(bits&0x3fff)
				height := 1 + int((bits>>14)&0x3fff)
				return width, height
			}
		case "VP8 ":
			if len(chunk) >= 10 && chunk[3] == 0x9d && chunk[4] == 0x01 && chunk[5] == 0x2a {
				width := int(binary.LittleEndian.Uint16(chunk[6:8]) & 0x3fff)
				height := int(binary.LittleEndian.Uint16(chunk[8:10]) & 0x3fff)
				return width, height
			}
		}
		offset = payload + chunkSize
		if chunkSize%2 != 0 {
			offset++
		}
	}
	return 0, 0
}
