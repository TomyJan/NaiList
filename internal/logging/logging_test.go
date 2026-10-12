package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestLevelsAndRedaction(t *testing.T) {
	var out bytes.Buffer
	logger, err := New(&out, "info", "configured-secret", "postgres://user:db-secret@localhost/nailist")
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug("hidden debug")
	logger.Info("startup", "version", "0.1.2")
	logger.Error("request failed", "error", &url.Error{Op: "Get", URL: "https://graph.qq.com/token?client_secret=url-secret&code=oauth-code#fragment-secret", Err: errors.New("connection refused")})
	logger.Error("database", "error", errors.New("password=db-secret configured-secret"))
	logger.Error("payload", "error", errors.New(`{"access_token":"access-secret","refresh_token":"refresh-secret","id_token":"id-secret"} Authorization: Bearer bearer-secret`))
	logger.Error("oauth", "error", &oauth2.RetrieveError{Response: &http.Response{StatusCode: 401}, Body: []byte("provider response secret content"), ErrorCode: "invalid_client"})
	logger.Error("discovery", "error", errors.New("502 Bad Gateway: discovery-response-secret"))
	logger.Error("jwks status", "error", errors.New("fetching keys oidc: get keys failed: 503 Service Unavailable keys-response-secret"))
	logger.Error("jwks JSON", "error", errors.New("fetching keys oidc: failed to decode keys: invalid character keys-decode-secret"))
	text := out.String()
	for _, secret := range []string{"hidden debug", "configured-secret", "db-secret", "url-secret", "oauth-code", "fragment-secret", "access-secret", "refresh-secret", "id-secret", "bearer-secret", "provider response secret content", "discovery-response-secret", "keys-response-secret", "keys-decode-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("sensitive/noisy value leaked: %s\n%s", secret, text)
		}
	}
	for _, useful := range []string{"level=INFO", "level=ERROR", "version=0.1.2", "graph.qq.com/token", "connection refused", "401", "invalid_client"} {
		if !strings.Contains(text, useful) {
			t.Fatalf("missing diagnostic %s: %s", useful, text)
		}
	}
	debug, err := New(&out, "DEBUG")
	if err != nil {
		t.Fatal(err)
	}
	debug.Log(t.Context(), slog.LevelDebug, "visible debug")
	if !strings.Contains(out.String(), "visible debug") {
		t.Fatal("DEBUG is not enabled")
	}
	if _, err := New(&out, "invalid"); err == nil {
		t.Fatal("invalid level accepted")
	}
}
