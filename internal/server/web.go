package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func serveWeb(w http.ResponseWriter, r *http.Request, dir string) {
	if r.Method != "GET" {
		http.NotFound(w, r)
		return
	}
	clean := filepath.Clean("/" + r.URL.Path)
	name := filepath.Join(dir, strings.TrimLeft(clean, "/\\"))
	if info, err := os.Stat(name); err == nil && !info.IsDir() {
		http.ServeFile(w, r, name)
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/manage" {
		http.NotFound(w, r)
		return
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		http.Error(w, "前端未构建，请运行 pnpm run build", 503)
		return
	}
	http.ServeFile(w, r, filepath.Join(dir, "index.html"))
}
