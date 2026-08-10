package taskassets

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/leonardo2api/leonardo2api/internal/domain"
)

func TestSaveOpenRemove(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	header := fileHeader(t, "reference.mp4", []byte("video-bytes"))
	asset, err := store.Save(header, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if asset.Path == "" || asset.SHA256 == "" || asset.Size != 11 {
		t.Fatalf("unexpected asset: %+v", asset)
	}
	file, err := store.Open(asset)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, asset.Size)
	_, _ = file.Read(data)
	file.Close()
	if !bytes.Equal(data, []byte("video-bytes")) {
		t.Fatalf("unexpected data %q", data)
	}
	if err := store.Remove(asset); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(asset); !os.IsNotExist(err) {
		t.Fatalf("expected removed file, got %v", err)
	}
}

func TestRejectsOversizedAndEscapingAsset(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	header := fileHeader(t, "large.bin", []byte("12345"))
	if _, err := store.Save(header, 4); err == nil {
		t.Fatal("expected size error")
	}
	if _, err := store.Open(domain.SourceMedia{Path: "../outside"}); err == nil {
		t.Fatal("expected path validation error")
	}
}

func fileHeader(t *testing.T, filename string, content []byte) *multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(1024); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { request.MultipartForm.RemoveAll() })
	return request.MultipartForm.File["file"][0]
}
