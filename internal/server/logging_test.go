package server

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"nailist/internal/groups"
	"nailist/internal/logging"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func captureLogs(t *testing.T, level string) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	logger, err := logging.New(&out, level)
	if err != nil {
		t.Fatal(err)
	}
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &out
}

type brokenGroups struct {
	memoryGroups
	panicValue bool
}

func (s *brokenGroups) List(context.Context, int64) ([]groups.Group, error) {
	if s.panicValue {
		panic("access_token=panic-secret")
	}
	return nil, errors.New("database unavailable")
}

func TestRequestLogsAndNoise(t *testing.T) {
	out := captureLogs(t, "info")
	handler := New(Config{}, nil, &memoryGroups{}).Routes()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/groups?code=query-secret", nil)
	r.Header.Set("Authorization", "Bearer header-secret")
	r.AddCookie(&http.Cookie{Name: "session", Value: "cookie-secret"})
	handler.ServeHTTP(w, r)
	text := out.String()
	for _, want := range []string{"level=INFO", "method=GET", "path=/api/groups", "status=200", "duration_ms=", "request_id="} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s: %s", want, text)
		}
	}
	if w.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing response request ID")
	}
	for _, secret := range []string{"query-secret", "header-secret", "cookie-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("request secret leaked: %s", text)
		}
	}
	out.Reset()
	for _, path := range []string{"/api/health", "/nailong.png"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	if out.Len() != 0 {
		t.Fatalf("routine polling/static requests polluted INFO: %s", out.String())
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/missing", nil))
	if !strings.Contains(out.String(), "level=WARN") {
		t.Fatal("4xx request not logged at WARN")
	}
}

func TestFailuresAndPanicsAreCorrelated(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(map[bool]string{false: "database", true: "panic"}[panics], func(t *testing.T) {
			out := captureLogs(t, "info")
			handler := New(Config{}, nil, &brokenGroups{panicValue: panics}).Routes()
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/groups?access_token=query-secret", nil))
			if w.Code != 500 {
				t.Fatalf("status=%d", w.Code)
			}
			text := out.String()
			if !strings.Contains(text, "level=ERROR") || !strings.Contains(text, "request_id="+w.Header().Get("X-Request-ID")) || strings.Contains(text, "panic-secret") || strings.Contains(text, "query-secret") {
				t.Fatalf("missing/unsafe failure log: %s", text)
			}
			if panics && !strings.Contains(text, "stack=") {
				t.Fatal("panic has no stack trace")
			}
		})
	}
}

func TestOAuthFailureHasStageWithoutSecrets(t *testing.T) {
	out := captureLogs(t, "info")
	handler := New(Config{QQAppID: "app", QQAppKey: "secret"}, nil, &memoryGroups{}).Routes()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/auth/qq/callback?code=code-secret&state=state-secret", nil))
	text := out.String()
	if !strings.Contains(text, "level=WARN") || !strings.Contains(text, "provider=qq") || !strings.Contains(text, "stage=state_validation") || strings.Contains(text, "code-secret") || strings.Contains(text, "state-secret") {
		t.Fatalf("unhelpful OAuth log: %s", text)
	}
}

type disconnectedWriter struct{ header http.Header }

func (w *disconnectedWriter) Header() http.Header       { return w.header }
func (w *disconnectedWriter) WriteHeader(int)           {}
func (w *disconnectedWriter) Write([]byte) (int, error) { return 0, errors.New("client disconnected") }

func TestResponseWriteFailureIsLogged(t *testing.T) {
	out := captureLogs(t, "info")
	handler := New(Config{}, nil, &memoryGroups{}).Routes()
	w := &disconnectedWriter{header: make(http.Header)}
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/groups", nil))
	if text := out.String(); !strings.Contains(text, "level=WARN") || !strings.Contains(text, "client disconnected") || !strings.Contains(text, "request_id="+w.Header().Get("X-Request-ID")) {
		t.Fatalf("missing response write error: %s", text)
	}
}
