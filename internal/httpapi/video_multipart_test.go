package httpapi

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/leonardo2api/leonardo2api/internal/config"
	"github.com/leonardo2api/leonardo2api/internal/taskassets"
)

func TestParseVideoMultipartMediaReferences(t *testing.T) {
	assets, err := taskassets.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields := map[string]string{
		"model": "seedance-2.0-fast", "prompt": "test", "duration": "8",
		"size": "1280x720", "resolution": "720p", "reference_strength": "HIGH", "generate_audio": "false",
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	writeFile := func(field, filename string, data []byte) {
		part, partErr := writer.CreateFormFile(field, filename)
		if partErr != nil {
			t.Fatal(partErr)
		}
		if _, partErr = part.Write(data); partErr != nil {
			t.Fatal(partErr)
		}
	}
	jpeg := append([]byte{0xff, 0xd8, 0xff, 0xe0}, bytes.Repeat([]byte{0}, 32)...)
	writeFile("image[]", "reference.jpg", jpeg)
	writeFile("start_frame", "start.jpg", jpeg)
	writeFile("end_frame", "end.jpg", jpeg)
	writeFile("video[]", "motion.mp4", append([]byte{0, 0, 0, 24}, []byte("ftypisom0000")...))
	writeFile("audio", "sound.mp3", append([]byte("ID3"), bytes.Repeat([]byte{0}, 32)...))
	writeFile("audio[]", "voice.mp3", append([]byte("ID3"), bytes.Repeat([]byte{1}, 32)...))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/v1/videos/generations", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	server := &Server{Assets: assets, Config: config.Config{MaxImageBytes: 1 << 20, MaxVideoBytes: 1 << 20, MaxAudioBytes: 1 << 20}}
	parsed, err := server.parseVideoMultipart(request)
	if err != nil {
		t.Fatal(err)
	}
	defer assets.CleanupVideoRequest(parsed)
	if len(parsed.ReferenceImages) != 1 || parsed.StartFrame == nil || parsed.EndFrame == nil || len(parsed.ReferenceVideos) != 1 || len(parsed.ReferenceAudios) != 2 {
		t.Fatalf("unexpected request: %+v", parsed)
	}
	if parsed.GenerateAudio == nil || *parsed.GenerateAudio {
		t.Fatalf("unexpected generate_audio: %+v", parsed.GenerateAudio)
	}
	for _, asset := range append(parsed.ReferenceImages, parsed.ReferenceVideos...) {
		if asset.Path == "" || asset.SHA256 == "" {
			t.Fatalf("asset metadata incomplete: %+v", asset)
		}
	}
}
