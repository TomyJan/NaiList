package server

import (
	"io/fs"
	"nailist/web"
	"net/http"
	"path"
	"strings"
)

func serveWeb(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.NotFound(w, r)
		return
	}
	files := web.Files()
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if info, err := fs.Stat(files, name); err == nil && !info.IsDir() {
		http.ServeFileFS(w, r, files, name)
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/manage" {
		http.NotFound(w, r)
		return
	}
	if _, err := fs.Stat(files, "index.html"); err != nil {
		http.Error(w, "前端未构建，请运行 pnpm run build", 503)
		return
	}
	http.ServeFileFS(w, r, files, "index.html")
}
