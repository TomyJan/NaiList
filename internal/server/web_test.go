package server

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

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
