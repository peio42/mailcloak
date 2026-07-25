package mailcloak

import (
	"embed"
	"net/http"
	"path"
	"strings"
)

//go:embed adminui/*
var adminUIFS embed.FS

func serveAdminUI(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}

	switch {
	case r.URL.Path == "/" || r.URL.Path == "/index.html":
		http.ServeFileFS(w, r, adminUIFS, "adminui/index.html")
		return true
	case strings.HasPrefix(r.URL.Path, "/assets/"):
		asset := path.Clean(strings.TrimPrefix(r.URL.Path, "/assets/"))
		if asset == "." || strings.HasPrefix(asset, "../") || strings.HasPrefix(asset, "/") {
			http.NotFound(w, r)
			return true
		}
		http.ServeFileFS(w, r, adminUIFS, "adminui/"+asset)
		return true
	default:
		return false
	}
}
