package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"nailist/internal/logging"
)

func startupTest(t *testing.T) *bytes.Buffer {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, key := range []string{"LOG_LEVEL", "CLIENT_IP_HEADER", "QQ_APP_ID", "QQ_APP_KEY", "QQ_USE_UNIONID", "OIDC_ID", "OIDC_NAME", "OIDC_ICON", "OIDC_ISSUER", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "OIDC_SCOPES", "UMAMI_SCRIPT_URL", "UMAMI_WEBSITE_ID"} {
		t.Setenv(key, "")
	}
	t.Setenv("DATABASE_URL", "postgres://user:db-secret@127.0.0.1:1/nailist?sslmode=disable&connect_timeout=1")
	t.Setenv("PUBLIC_URL", "http://localhost:8080")
	t.Setenv("PORT", "8080")
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &bytes.Buffer{}
}

func TestStartupFailuresAreLogged(t *testing.T) {
	for _, tc := range []struct{ name, key, value, stage string }{
		{"missing database", "DATABASE_URL", "", "config"},
		{"invalid port", "PORT", "invalid", "config"},
		{"invalid client IP header", "CLIENT_IP_HEADER", "invalid header", "config"},
		{"invalid log level", "LOG_LEVEL", "invalid", "logging_config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := startupTest(t)
			t.Setenv(tc.key, tc.value)
			if err := run(t.Context(), out); err == nil {
				t.Fatal("startup unexpectedly succeeded")
			}
			if text := out.String(); !strings.Contains(text, "level=ERROR") || !strings.Contains(text, "stage="+tc.stage) || strings.Contains(text, "HTTP 服务就绪") {
				t.Fatalf("unhelpful startup output: %s", text)
			}
		})
	}
	t.Run("invalid dotenv", func(t *testing.T) {
		out := startupTest(t)
		if err := os.WriteFile(".env", []byte("QQ_APP_KEY='dotenv-secret"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := run(t.Context(), out); err == nil {
			t.Fatal("bad dotenv accepted")
		}
		if text := out.String(); !strings.Contains(text, "stage=environment") || strings.Contains(text, "dotenv-secret") {
			t.Fatalf("unsafe dotenv error: %s", text)
		}
	})
	t.Run("occupied port", func(t *testing.T) {
		out := startupTest(t)
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		t.Setenv("PORT", fmt.Sprint(listener.Addr().(*net.TCPAddr).Port))
		if err := run(t.Context(), out); err == nil {
			t.Fatal("occupied port accepted")
		}
		if text := out.String(); !strings.Contains(text, "stage=http_bind") || !strings.Contains(text, "level=ERROR") || strings.Contains(text, "正在连接数据库") || strings.Contains(text, "HTTP 服务就绪") {
			t.Fatalf("wrong startup order: %s", text)
		}
	})
	t.Run("database unavailable", func(t *testing.T) {
		out := startupTest(t)
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		listener.Close()
		t.Setenv("PORT", fmt.Sprint(port))
		if err := run(t.Context(), out); err == nil {
			t.Fatal("missing database accepted")
		}
		if text := out.String(); !strings.Contains(text, "stage=database_connect") || strings.Contains(text, "db-secret") || strings.Contains(text, "HTTP 服务就绪") {
			t.Fatalf("unsafe database error: %s", text)
		}
		// A startup failure must release the already-bound HTTP port.
		listener, err = net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			t.Fatal(err)
		}
		listener.Close()
	})
}

func TestStartupFailureHasSemanticContext(t *testing.T) {
	out := startupTest(t)
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("PORT", fmt.Sprint(listener.Addr().(*net.TCPAddr).Port))
	if err := run(t.Context(), out); err == nil {
		t.Fatal("port conflict accepted")
	}
	for _, want := range []string{"service=nailist", "event=app.starting", "event=app.startup.failed", "stage=http_bind", "listen_address=", "hint=", "error_kind=network", "pid=", "version="} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %s: %s", want, out.String())
		}
	}
}

func TestEnvironmentReadFailureRetainsSafeCause(t *testing.T) {
	out := startupTest(t)
	if err := os.Mkdir(".env", 0700); err != nil {
		t.Fatal(err)
	}
	err := run(t.Context(), out)
	var path *os.PathError
	if !errors.As(err, &path) || !strings.Contains(out.String(), "operation=") || !strings.Contains(out.String(), "stage=environment") {
		t.Fatalf("environment IO cause lost: %v %s", err, out.String())
	}
}

func TestStartupCanceledIsNotError(t *testing.T) {
	out := startupTest(t)
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORT", fmt.Sprint(listener.Addr().(*net.TCPAddr).Port))
	listener.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := run(ctx, out); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected startup cancellation: %v", err)
	}
	if strings.Contains(out.String(), "level=ERROR") || !strings.Contains(out.String(), "event=app.startup.canceled") || !strings.Contains(out.String(), "outcome=canceled") {
		t.Fatalf("canceled startup misreported: %s", out.String())
	}
}

func TestShutdownWaitsForActiveRequests(t *testing.T) {
	var out bytes.Buffer
	logger, _ := logging.New(&out, "info")
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.Write([]byte("completed"))
	})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serveHTTP(ctx, server, listener) }()
	response := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		res, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			defer res.Body.Close()
			var body []byte
			body, err = io.ReadAll(res.Body)
			if err == nil && string(body) != "completed" {
				err = fmt.Errorf("incomplete response: %q", body)
			}
		}
		response <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("shutdown abandoned active request: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-response:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request did not finish")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	if !strings.Contains(out.String(), "HTTP 服务已停止") {
		t.Fatal("missing shutdown log")
	}
}
