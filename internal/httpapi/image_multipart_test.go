package httpapi

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/leonardo2api/leonardo2api/internal/config"
	"github.com/leonardo2api/leonardo2api/internal/taskassets"
)

func TestParseAsyncImageMultipartReferences(t *testing.T) {
	assets, err := taskassets.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := map[string]string{
		"model": "adobe/gpt-image-2", "prompt": "restyle", "size": "1024x1024",
		"n": "1", "quality": "low", "response_format": "url", "reference_strength": "HIGH",
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("image[]", "reference.png")
	if err != nil {
		t.Fatal(err)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	if _, err = part.Write(png); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest("POST", "/v1/images/generations", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	server := &Server{Assets: assets, Config: config.Config{MaxImageBytes: 1 << 20, MaxMultipartBytes: 8 << 20}}
	parsed, err := server.parseAsyncImageMultipart(request)
	if err != nil {
		t.Fatal(err)
	}
	defer assets.CleanupImageRequest(parsed)
	if len(parsed.ReferenceImages) != 1 || parsed.ReferenceStrength != "HIGH" || parsed.ResponseFormat != "url" {
		t.Fatalf("unexpected request: %+v", parsed)
	}
	if parsed.Provider != "" {
		t.Fatalf("public multipart parser must not populate provider: %q", parsed.Provider)
	}
	asset := parsed.ReferenceImages[0]
	if asset.Path == "" || asset.SHA256 == "" || asset.MediaType != "image/png" {
		t.Fatalf("asset metadata incomplete: %+v", asset)
	}
}
