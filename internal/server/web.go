package server

import (
	"bytes"
	"errors"
	"html"
	"io/fs"
	"log/slog"
	"nailist/internal/traffic"
	"nailist/web"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

func umamiOrigin(cfg Config) (string, error) {
	if cfg.UmamiScriptURL == "" && cfg.UmamiWebsiteID == "" {
		return "", nil
	}
	u, err := url.ParseRequestURI(cfg.UmamiScriptURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" || strings.ContainsAny(u.Host, " \t\r\n;,'\"<>\\") || !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(cfg.UmamiWebsiteID) {
		return "", errors.New("UMAMI_SCRIPT_URL 和 UMAMI_WEBSITE_ID 须同时配置，脚本地址须为 HTTP(S) URL，网站 ID 只能包含字母、数字、横线和下划线")
	}
	return u.Scheme + "://" + u.Host, nil
}

func ValidateWebConfig(cfg Config) error {
	_, err := umamiOrigin(cfg)
	if err == nil && strings.TrimSpace(cfg.UmamiShareURL) != "" {
		_, err = traffic.New(cfg.UmamiShareURL)
	}
	return err
}

// Capture the configured HTML once at startup; assets remain embedded and unchanged.
func newWebHandler(cfg Config) (http.Handler, string) {
	origin, _ := umamiOrigin(cfg)
	configErr := ValidateWebConfig(cfg)
	extra := ""
	if origin != "" {
		extra = " " + origin
	}
	audioOrigin := "https://cdn.amoe.cc"
	audioPath := "https://cdn.amoe.cc/web-static/private/tomyjan-website/nailong/audio/"
	csp := "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'" + extra + "; style-src 'self' https://cdn.amoe.cc; font-src 'self' https://cdn-file.hyperos.mi.com; img-src 'self' https: data: blob:; connect-src 'self'" + extra + " " + audioOrigin + "; media-src 'self' blob: " + audioPath + "; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
	files := web.Files()
	index, err := fs.ReadFile(files, "index.html")
	marker := "<!--__NAILIST_RUNTIME__-->"
	if err == nil && !bytes.Contains(index, []byte(marker)) {
		err = errors.New("missing runtime placeholder")
	}
	if err != nil {
		slog.Error("准备内嵌前端失败", "error", err)
	}
	if configErr != nil {
		slog.Error("前端运行配置无效", "error", configErr)
	}
	runtimeHTML := `<meta name="nailist-version" content="` + html.EscapeString(cfg.Version) + `" />`
	if strings.TrimSpace(cfg.UmamiShareURL) != "" && configErr == nil {
		runtimeHTML += `<meta name="nailist-traffic" content="enabled" />`
	}
	if origin != "" {
		runtimeHTML += `<script defer src="` + html.EscapeString(cfg.UmamiScriptURL) + `" data-website-id="` + html.EscapeString(cfg.UmamiWebsiteID) + `"></script>`
	}
	index = bytes.Replace(index, []byte(marker), []byte(runtimeHTML), 1)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/manage" || r.URL.Path == "/admin" || r.URL.Path == "/admin/" || r.URL.Path == "/admin/users" || r.URL.Path == "/admin/groups" || r.URL.Path == "/admin/sites" || r.URL.Path == "/index.html" {
			if err != nil || configErr != nil {
				http.Error(w, "前端或统计配置无效，请检查配置并重新构建前端", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if info, err := fs.Stat(files, name); err == nil && !info.IsDir() {
			http.ServeFileFS(w, r, files, name)
			return
		}
		http.NotFound(w, r)
	}), csp
}
