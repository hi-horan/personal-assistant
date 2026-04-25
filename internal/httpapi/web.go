package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed web/*
var webFS embed.FS

func (s *Server) webIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	serveEmbeddedFile(w, r, "index.html")
}

func (s *Server) webAsset(w http.ResponseWriter, r *http.Request) {
	file := path.Clean(r.PathValue("file"))
	if file == "." || strings.HasPrefix(file, "../") || strings.Contains(file, "/") {
		http.NotFound(w, r)
		return
	}
	serveEmbeddedFile(w, r, file)
}

func serveEmbeddedFile(w http.ResponseWriter, r *http.Request, file string) {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		http.Error(w, "web assets unavailable", http.StatusInternalServerError)
		return
	}
	http.ServeFileFS(w, r, sub, file)
}
