package server

import (
	"bytes"
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"log/slog"
	"nailist/internal/groups"
	"nailist/internal/logging"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSemanticRequestLogsKeepAuthenticatedUser(t *testing.T) {
	out := captureLogs(t, "debug")
	handler := New(Config{}, nil, &memoryGroups{items: []groups.Group{{ID: 42, OwnerID: 8}}}).Routes()
	r := httptest.NewRequest("GET", "/api/my/groups", nil)
	r = r.WithContext(context.WithValue(r.Context(), userKey, User{ID: 8}))
	r.Header.Set("X-Request-ID", "untrusted-request-secret")
	r.Header.Set("X-Forwarded-For", "spoofed-client-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	text := out.String()
	for _, want := range []string{"event=http.request.started", "event=groups.list.completed", "event=http.request.completed", "operation=groups.mine", "user_id=8", "route=/api/my/groups", "count=1", "outcome=success", "peer_ip="} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s: %s", want, text)
		}
	}
	for _, secret := range []string{"untrusted-request-secret", "spoofed-client-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("untrusted metadata logged: %s", text)
		}
	}
}

func TestGroupRejectionHasActionReasonAndSummary(t *testing.T) {
	out := captureLogs(t, "debug")
	handler := New(Config{PublicURL: "http://localhost:8080"}, nil, &memoryGroups{}).Routes()
	r := httptest.NewRequest("POST", "/api/groups", strings.NewReader(`{"organization":"学校","qqNumber":"12","joinUrl":"https://qm.qq.com/q/test"}`))
	r = r.WithContext(context.WithValue(r.Context(), userKey, User{ID: 8, CSRF: "right"}))
	r.Header.Set("X-CSRF-Token", "right")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	text := out.String()
	for _, want := range []string{"event=groups.save.rejected", "operation=groups.create", "reason=validation_failed", "user_id=8", "outcome=rejected", "status=400"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s: %s", want, text)
		}
	}
}

func TestRequestSummaryDoesNotDuplicateInternalError(t *testing.T) {
	out := captureLogs(t, "info")
	New(Config{}, nil, &brokenGroups{}).Routes().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/groups", nil))
	if strings.Count(out.String(), "level=ERROR") != 1 {
		t.Fatalf("internal error must be logged once: %s", out.String())
	}
	for _, want := range []string{"event=groups.list.failed", "operation=groups.list", "reason=store_failed", "error_kind=internal"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %s: %s", want, out.String())
		}
	}
}

func TestRequestCancellationIsNotAnInternalError(t *testing.T) {
	out := captureLogs(t, "debug")
	r := chi.NewRouter()
	r.Use(logRequest)
	r.Get("/cancel", func(w http.ResponseWriter, r *http.Request) { failure(w, r, context.Canceled) })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/cancel", nil).WithContext(ctx))
	if strings.Contains(out.String(), "level=ERROR") || !strings.Contains(out.String(), "reason=client_canceled") {
		t.Fatalf("wrong cancellation classification: %s", out.String())
	}
}

func TestRequestDeadlineHasWarnSummary(t *testing.T) {
	out := captureLogs(t, "debug")
	h := logRequest(recoverRequest(requestTimeout(time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/groups", nil))
	if w.Code != 504 || strings.Contains(out.String(), "level=ERROR") || !strings.Contains(out.String(), "outcome=timeout") || !strings.Contains(out.String(), "level=WARN") {
		t.Fatalf("deadline classification/status incorrect: status=%d %s", w.Code, out.String())
	}
}

func TestPostgresHealthCancellationAndTimeoutLevels(t *testing.T) {
	db := authTestDB(t)
	for _, mode := range []string{"canceled", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			out := captureLogs(t, "debug")
			ctx, cancel := context.WithCancel(t.Context())
			if mode == "timeout" {
				cancel()
				ctx, cancel = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
			}
			cancel()
			New(Config{}, db, &memoryGroups{}).Routes().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/health", nil).WithContext(ctx))
			if strings.Contains(out.String(), "level=ERROR") || !strings.Contains(out.String(), "outcome="+mode) {
				t.Fatalf("health failure misclassified: %s", out.String())
			}
		})
	}
}

func TestRequestPanicAfterWriteKeepsOriginalBody(t *testing.T) {
	out := captureLogs(t, "debug")
	h := logRequest(recoverRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial"))
		panic("unexpected failure")
	})))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/groups", nil))
	if w.Body.String() != "partial" || strings.Count(out.String(), "level=ERROR") != 1 || !strings.Contains(out.String(), "outcome=failed") {
		t.Fatalf("panic response/log incorrect: %s %s", w.Body.String(), out.String())
	}
}

func TestRequestAbortIsNotSuccess(t *testing.T) {
	out := captureLogs(t, "debug")
	h := logRequest(recoverRequest(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })))
	func() {
		defer func() {
			if recover() != http.ErrAbortHandler {
				t.Fatal("abort must propagate to net/http")
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/groups", nil))
	}()
	if strings.Contains(out.String(), "status=200") || strings.Contains(out.String(), "level=ERROR") || !strings.Contains(out.String(), "outcome=canceled") || !strings.Contains(out.String(), "response_started=false") {
		t.Fatalf("abort reported success: %s", out.String())
	}
}

func TestRawResponseWriteFailureIsLoggedOnce(t *testing.T) {
	out := captureLogs(t, "debug")
	h := logRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("first"))
		_, _ = w.Write([]byte("second"))
	}))
	h.ServeHTTP(&disconnectedWriter{header: make(http.Header)}, httptest.NewRequest("GET", "/asset.js", nil))
	if strings.Count(out.String(), "event=http.response.write_failed") != 1 || !strings.Contains(out.String(), "outcome=failed") || !strings.Contains(out.String(), "reason=response_write_failed") {
		t.Fatalf("raw response failure unobserved/duplicated: %s", out.String())
	}
}

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
	if !strings.Contains(out.String(), "level=INFO") || !strings.Contains(out.String(), "reason=route_not_found") {
		t.Fatal("unmatched route must have an INFO summary and a specific reason")
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
