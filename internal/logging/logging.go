// Package logging configures the application's stderr diagnostics and redacts credentials.
package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/oauth2"
)

var (
	urls             = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s"<>]+`)
	credentials      = regexp.MustCompile(`(?i)("?(?:access_token|refresh_token|id_token|client_secret|password|code|state|nonce|verifier)"?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;}]+)`)
	bearer           = regexp.MustCompile(`(?i)Bearer\s+[^\s",;]+`)
	jwt              = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	providerHTTP     = regexp.MustCompile(`(?s)^(\d{3} [^:\n]+):.*$`)
	providerKeysHTTP = regexp.MustCompile(`(?s)(oidc: get keys failed: \d{3}).*`)
)

// New creates a text logger. Secret values and connection-string passwords are
// removed even when a dependency includes them in an error message.
func New(w io.Writer, level string, secrets ...string) (*slog.Logger, error) {
	var threshold slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		threshold = slog.LevelInfo
	case "debug":
		threshold = slog.LevelDebug
	case "warn":
		threshold = slog.LevelWarn
	case "error":
		threshold = slog.LevelError
	default:
		return nil, errors.New("LOG_LEVEL 必须为 debug、info、warn 或 error")
	}
	var values []string
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		values = append(values, secret)
		if u, err := url.Parse(secret); err == nil && u.User != nil {
			if password, ok := u.User.Password(); ok && password != "" {
				values = append(values, password, url.QueryEscape(password))
			}
		}
		for _, match := range credentials.FindAllStringSubmatch(secret, -1) {
			value := strings.Trim(strings.TrimSpace(strings.TrimPrefix(match[0], match[1])), "\"'")
			if value != "" {
				values = append(values, value)
			}
		}
	}
	clean := func(s string) string {
		for _, secret := range values {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
		s = urls.ReplaceAllStringFunc(s, func(raw string) string {
			u, err := url.Parse(raw)
			if err != nil {
				return "[REDACTED URL]"
			}
			u.User, u.RawQuery, u.Fragment = nil, "", ""
			u.ForceQuery = false
			return u.String()
		})
		s = credentials.ReplaceAllString(s, "${1}[REDACTED]")
		s = bearer.ReplaceAllString(s, "Bearer [REDACTED]")
		return jwt.ReplaceAllString(s, "[REDACTED JWT]")
	}
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{Level: threshold, ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Value.Kind() == slog.KindAny {
			if err, ok := a.Value.Any().(error); ok {
				var oauth *oauth2.RetrieveError
				var pg *pgconn.PgError
				switch {
				case errors.As(err, &oauth):
					status := 0
					if oauth.Response != nil {
						status = oauth.Response.StatusCode
					}
					a.Value = slog.StringValue(fmt.Sprintf("OAuth token exchange: status=%d error_type=%s", status, clean(oauth.ErrorCode)))
				case errors.As(err, &pg):
					a.Value = slog.StringValue(clean(fmt.Sprintf("PostgreSQL: %s (SQLSTATE %s)", pg.Message, pg.Code)))
				default:
					message := err.Error()
					// go-oidc includes arbitrary response bodies in these errors.
					message = providerHTTP.ReplaceAllString(message, "${1}: [response body omitted]")
					message = providerKeysHTTP.ReplaceAllString(message, "${1} [response body omitted]")
					if i := strings.Index(message, "oidc: failed to decode keys:"); i >= 0 {
						message = message[:i] + "oidc: failed to decode keys [response body omitted]"
					}
					a.Value = slog.StringValue(clean(message))
				}
			}
		}
		if a.Value.Kind() == slog.KindString {
			a.Value = slog.StringValue(clean(a.Value.String()))
		}
		return a
	}})
	return slog.New(handler), nil
}
