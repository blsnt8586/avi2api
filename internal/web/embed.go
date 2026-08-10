package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed dist/*
var content embed.FS

func Handler() http.Handler {
	sub, _ := fs.Sub(content, "dist")
	index, _ := fs.ReadFile(sub, "index.html")
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin/api") || strings.HasPrefix(r.URL.Path, "/v1/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" || path.Ext(r.URL.Path) == "" {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(index)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}
