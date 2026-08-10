package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerCachePolicy(t *testing.T) {
	handler := Handler()

	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if got := index.Header().Get("Cache-Control"); got != "no-cache, no-store, must-revalidate" {
		t.Fatalf("index cache-control=%q", got)
	}

	assetName := ""
	for _, token := range strings.Fields(string(index.Body.Bytes())) {
		if strings.Contains(token, "/assets/") {
			start := strings.Index(token, "/assets/")
			end := strings.IndexAny(token[start:], "\"'")
			if end > 0 {
				assetName = token[start : start+end]
				break
			}
		}
	}
	if assetName == "" {
		t.Fatal("embedded index does not reference an asset")
	}
	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, assetName, nil))
	if got := asset.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache-control=%q", got)
	}
}
