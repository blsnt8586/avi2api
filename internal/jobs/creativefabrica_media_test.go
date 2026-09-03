package jobs

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestCreativeFabricaImageDimensionsReadsPNG(t *testing.T) {
	var buffer bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 17, 9))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	width, height := creativeFabricaImageDimensions(buffer.Bytes(), "image/png", "frame.png")
	if width != 17 || height != 9 {
		t.Fatalf("dimensions = %dx%d, want 17x9", width, height)
	}
}

func TestCreativeFabricaImageDimensionsReadsWebPVP8X(t *testing.T) {
	raw := make([]byte, 30)
	copy(raw[:4], "RIFF")
	copy(raw[8:12], "WEBP")
	copy(raw[12:16], "VP8X")
	binary.LittleEndian.PutUint32(raw[16:20], 10)
	raw[24] = 0x7f
	raw[25] = 0x02
	raw[27] = 0x67
	raw[28] = 0x01
	width, height := creativeFabricaImageDimensions(raw, "image/webp", "frame.webp")
	if width != 640 || height != 360 {
		t.Fatalf("dimensions = %dx%d, want 640x360", width, height)
	}
}

func TestCreativeFabricaImageDimensionsUnknownReturnsZero(t *testing.T) {
	width, height := creativeFabricaImageDimensions([]byte("not-an-image"), "image/png", "frame.png")
	if width != 0 || height != 0 {
		t.Fatalf("dimensions = %dx%d, want 0x0", width, height)
	}
}
