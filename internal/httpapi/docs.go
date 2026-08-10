package httpapi

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.json
var openAPI []byte

func openAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openAPI)
}
