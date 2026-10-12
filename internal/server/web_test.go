package server

import (
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestStartupWebInjection(t *testing.T) {
	cfg := Config{Version: "0.1.1-abcdef0", UmamiScriptURL: "https://stats.example.com/script.js?x=1&y=2", UmamiWebsiteID: "00000000-0000-0000-0000-000000000001"}
	handler := New(cfg, nil, &memoryGroups{}).Routes()
	// Runtime configuration is captured at construction, not read on each request.
	t.Setenv("UMAMI_SCRIPT_URL", "https://changed.example.com/script.js")
	var first string
	for _, route := range []string{"/", "/manage", "/index.html"} {
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, httptest.NewRequest("GET", route, nil))
		body := out.Body.String()
		if out.Code != 200 || !strings.Contains(body, `name="nailist-version" content="0.1.1-abcdef0"`) || !strings.Contains(body, `src="`+html.EscapeString(cfg.UmamiScriptURL)+`"`) || !strings.Contains(body, `data-website-id="`+cfg.UmamiWebsiteID+`"`) || strings.Contains(body, "__NAILIST_RUNTIME__") || strings.Contains(body, "changed.example.com") {
			t.Fatalf("%s: %d %s", route, out.Code, body)
		}
		if first != "" && body != first {
			t.Fatal("all page routes must use the same startup HTML")
		}
		first = body
		csp := out.Header().Get("Content-Security-Policy")
		if strings.Contains(csp, "'unsafe-eval'") || strings.Contains(csp, "'unsafe-inline'") {
			t.Fatalf("WASM support must not enable inline scripts or JavaScript eval: %s", csp)
		}
		for _, directive := range []string{"script-src 'self' 'wasm-unsafe-eval' https://stats.example.com", "connect-src 'self' https://stats.example.com", "style-src 'self' https://cdn.amoe.cc", "font-src 'self' https://cdn-file.hyperos.mi.com", "img-src 'self' https: data: blob:"} {
			if !strings.Contains(csp, directive) {
				t.Fatalf("missing CSP directive %s: %s", directive, csp)
			}
		}
		if out.Header().Get("Cache-Control") != "no-cache" {
			t.Fatal("HTML must revalidate after restart")
		}
	}
	plain := httptest.NewRecorder()
	New(Config{Version: "0.1.1"}, nil, &memoryGroups{}).Routes().ServeHTTP(plain, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(plain.Body.String(), "data-website-id") || strings.Contains(plain.Header().Get("Content-Security-Policy"), "stats.example.com") {
		t.Fatal("disabled analytics must not inject a script or origin")
	}
}

func TestInvalidUmamiConfig(t *testing.T) {
	for _, cfg := range []Config{
		{UmamiScriptURL: "https://stats.example/script.js"},
		{UmamiWebsiteID: "abc"},
		{UmamiScriptURL: "javascript:alert(1)", UmamiWebsiteID: "abc"},
		{UmamiScriptURL: "https://user:pass@stats.example/script.js", UmamiWebsiteID: "abc"},
		{UmamiScriptURL: "https://stats.example;evil/script.js", UmamiWebsiteID: "abc"},
		{UmamiScriptURL: "https://stats.example/script.js", UmamiWebsiteID: `abc" onload="alert(1)`},
	} {
		if ValidateWebConfig(cfg) == nil {
			t.Fatalf("invalid config accepted: %+v", cfg)
		}
	}
	if err := ValidateWebConfig(Config{}); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddedWebWithoutExternalFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	handler := New(Config{}, nil, &memoryGroups{}).Routes()
	var index string
	for _, route := range []string{"/", "/manage"} {
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, httptest.NewRequest(http.MethodGet, route, nil))
		if out.Code != http.StatusOK || !strings.Contains(out.Body.String(), `<div id="root"></div>`) {
			t.Fatalf("%s: %d %s", route, out.Code, out.Body.String())
		}
		index = out.Body.String()
	}
	assets := regexp.MustCompile(`(?:src|href)="(/[^"?]+\.(?:js|css|ico|png))"`).FindAllStringSubmatch(index, -1)
	if len(assets) < 3 {
		t.Fatal("built frontend must include scripts, styles and an icon")
	}
	for _, asset := range assets {
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, httptest.NewRequest(http.MethodGet, asset[1], nil))
		if out.Code != http.StatusOK || out.Body.Len() == 0 || strings.HasPrefix(out.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("asset %s: %d %s", asset[1], out.Code, out.Header().Get("Content-Type"))
		}
	}
	for _, route := range []string{"/unknown", "/assets/missing.js", "/assets/", "/api/unknown", "/../../go.mod"} {
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, httptest.NewRequest(http.MethodGet, route, nil))
		if out.Code != http.StatusNotFound {
			t.Fatalf("%s: expected 404, got %d", route, out.Code)
		}
	}
}
