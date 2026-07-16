package webui

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// dist is replaced by the Vue production build during the Docker build.
// The checked-in placeholder keeps local Go builds and tests self-contained.
//
//go:embed dist
var dist embed.FS

type spaHandler struct {
	files  fs.FS
	server http.Handler
}

// Handler serves built frontend assets and falls back to index.html for
// client-side Vue Router routes.
func Handler() http.Handler {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return &spaHandler{files: root, server: http.FileServer(http.FS(root))}
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name != "" && name != "." {
		if info, err := fs.Stat(h.files, name); err == nil && !info.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			h.server.ServeHTTP(w, r)
			return
		}
	}
	if name == "api" || strings.HasPrefix(name, "api/") ||
		name == "callbacks" || strings.HasPrefix(name, "callbacks/") ||
		name == "healthz" || strings.HasPrefix(name, "assets/") {
		http.NotFound(w, r)
		return
	}

	index, err := fs.ReadFile(h.files, "index.html")
	if err != nil {
		http.Error(w, "frontend is not available", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", mime.TypeByExtension(".html"))
	http.ServeContent(w, r, "index.html", time.Time{}, strings.NewReader(string(index)))
}
