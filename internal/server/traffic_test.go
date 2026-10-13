package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrafficDisabled(t *testing.T) {
	handler := New(Config{}, nil, &memoryGroups{}).Routes()
	for _, path := range []string{"/", "/api/traffic"} {
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, httptest.NewRequest("GET", path, nil))
		if path == "/api/traffic" && out.Code != 404 {
			t.Fatalf("disabled endpoint: %d", out.Code)
		}
		if strings.Contains(out.Body.String(), `name="nailist-traffic"`) {
			t.Fatal("disabled footer must not be enabled")
		}
	}
}

func TestTrafficProxyAndPrivateInjection(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/share/") {
			fmt.Fprint(w, `{"websiteId":"website1","token":"private-token"}`)
			return
		}
		fmt.Fprint(w, `{"visitors":123,"pageviews":456}`)
	}))
	defer upstream.Close()
	cfg := Config{UmamiShareURL: upstream.URL + "/share/Secret123"}
	if err := ValidateWebConfig(cfg); err != nil {
		t.Fatal(err)
	}
	handler := New(cfg, nil, &memoryGroups{}).Routes()
	for _, path := range []string{"/", "/api/traffic?url=https://evil.test"} {
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, httptest.NewRequest("GET", path, nil))
		body := out.Body.String()
		if out.Code != 200 || strings.Contains(body, "Secret123") || strings.Contains(body, "private-token") || strings.Contains(body, upstream.URL) {
			t.Fatalf("response must not leak config: %d %s", out.Code, body)
		}
		if path == "/" {
			if !strings.Contains(body, `name="nailist-traffic" content="enabled"`) {
				t.Fatal("configured footer must be enabled")
			}
			if strings.Contains(out.Header().Get("Content-Security-Policy"), upstream.URL) {
				t.Fatal("share URL must not add frontend upstream access")
			}
		} else if !strings.Contains(body, `"online":123`) || !strings.Contains(body, `"today":{"visitors":123,"pageviews":456}`) || out.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("invalid summary or cache policy: %s", body)
		}
	}
	if ValidateWebConfig(Config{UmamiShareURL: "not-a-share"}) == nil {
		t.Fatal("startup must reject invalid share URL")
	}
}

func TestTrafficFailureIsSafe(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "private response", 403) }))
	defer upstream.Close()
	handler := New(Config{UmamiShareURL: upstream.URL + "/share/Secret123"}, nil, &memoryGroups{}).Routes()
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, httptest.NewRequest("GET", "/api/traffic", nil))
	if out.Code != 503 || !strings.Contains(out.Body.String(), "访问统计暂不可用") || strings.Contains(out.Body.String(), "private") {
		t.Fatalf("safe failure: %d %s", out.Code, out.Body.String())
	}
}
