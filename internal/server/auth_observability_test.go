package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"nailist/internal/logging"
	"net/url"
)

func authLogContext() (context.Context, *bytes.Buffer) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return logging.WithLogger(context.Background(), logger), &output
}

func requireAuthLog(t *testing.T, output *bytes.Buffer, event, stage, reason string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) == nil && record["event"] == event && (stage == "" || record["stage"] == stage) && (reason == "" || record["reason"] == reason) {
			return
		}
	}
	t.Fatalf("missing event=%s stage=%s reason=%s in %s", event, stage, reason, output)
}

func TestQQObservabilityStagesAndPrivateValues(t *testing.T) {
	ctx, output := authLogContext()
	s := New(Config{QQAppID: "app", QQAppKey: "private-client-secret", QQUseUnionID: true}, nil, &memoryGroups{})
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"access_token":"private-access-token"}`
		switch r.URL.Path {
		case "/oauth2.0/me":
			body = `{"client_id":"app","openid":"private-openid","unionid":"private-unionid"}`
		case "/user/get_user_info":
			body = `{"ret":0,"nickname":"private-nickname","figureurl_qq_2":"https://example.test/private-avatar"}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	if _, err := s.qqIdentity(ctx, "private-auth-code"); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"token", "identity", "profile"} {
		requireAuthLog(t, output, "auth.provider.started", stage, "")
		requireAuthLog(t, output, "auth.provider.completed", stage, "")
	}
	requireAuthLog(t, output, "upstream.request.completed", "", "")
	for _, secret := range []string{"private-access-token", "private-client-secret", "private-auth-code", "private-openid", "private-unionid", "private-nickname", "private-avatar"} {
		if strings.Contains(output.String(), secret) {
			t.Errorf("logs leaked %q: %s", secret, output)
		}
	}
}

func TestQQObservabilityRejectsProviderError(t *testing.T) {
	ctx, output := authLogContext()
	s := New(Config{QQAppID: "app", QQAppKey: "key"}, nil, &memoryGroups{})
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"error":100013,"error_description":"private-provider-response"}`)), Header: http.Header{}}, nil
	})}
	if _, err := s.qqIdentity(ctx, "private-code"); err == nil {
		t.Fatal("provider rejection accepted")
	}
	requireAuthLog(t, output, "auth.provider.completed", "token", "access_token_missing")
	if !strings.Contains(output.String(), `"provider_error_code":100013`) || strings.Contains(output.String(), "private-provider-response") {
		t.Fatal(output.String())
	}
}

func TestOIDCTokenRejectionSeverity(t *testing.T) {
	for _, code := range []string{"invalid_grant", "access_denied", "invalid_client", "server_error"} {
		t.Run(code, func(t *testing.T) {
			out := captureLogs(t, "debug")
			var upstream *httptest.Server
			upstream = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/.well-known/openid-configuration" {
					_ = json.NewEncoder(w).Encode(map[string]any{"issuer": upstream.URL, "authorization_endpoint": upstream.URL + "/authorize", "token_endpoint": upstream.URL + "/token", "jwks_uri": upstream.URL + "/keys"})
					return
				}
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": "private-response-description"})
			}))
			defer upstream.Close()
			s := New(Config{PublicURL: "https://groups.example", OIDC: OIDCConfig{ID: "campus", Name: "校园", Issuer: upstream.URL, ClientID: "client"}}, nil, &memoryGroups{})
			s.client = upstream.Client()
			_, err := s.oidcIdentity(t.Context(), "private-code", "private-nonce", "private-verifier")
			if err == nil {
				t.Fatal("failed token exchange accepted")
			}
			logAuthFailure(httptest.NewRequest("GET", "/auth/campus/callback", nil), "campus", "identity_exchange", "failed", err)
			rejected := code == "invalid_grant" || code == "access_denied"
			if strings.Contains(out.String(), "level=ERROR") == rejected || strings.Contains(out.String(), "private-response-description") {
				t.Fatalf("wrong classification/privacy: %s", out.String())
			}
			if rejected && (!strings.Contains(out.String(), "outcome=rejected") || !strings.Contains(out.String(), "reason=token_"+code)) {
				t.Fatalf("rejection reason missing: %s", out.String())
			}
		})
	}
}

func TestOIDCDiscoveryBodyInterruptionSeverity(t *testing.T) {
	for _, mode := range []string{"canceled", "timeout", "client_timeout"} {
		t.Run(mode, func(t *testing.T) {
			out := captureLogs(t, "debug")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "timeout" {
				ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
				defer cancel()
			}
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				if mode == "canceled" {
					cancel()
				}
				<-r.Context().Done()
			}))
			defer upstream.Close()
			s := New(Config{PublicURL: "https://groups.example", OIDC: OIDCConfig{ID: "campus", Name: "校园", Issuer: upstream.URL, ClientID: "client"}}, nil, &memoryGroups{})
			s.client = upstream.Client()
			if mode == "client_timeout" {
				s.client.Timeout = 100 * time.Millisecond
			}
			_, _, err := s.oidcClient(ctx)
			if err == nil {
				t.Fatal("interrupted response accepted")
			}
			logAuthFailure(httptest.NewRequest("GET", "/auth/campus/callback", nil).WithContext(ctx), "campus", "oidc_discovery", "failed", err)
			kind, level := "timeout", "WARN"
			if mode == "canceled" {
				kind, level = "canceled", "INFO"
			}
			if strings.Contains(out.String(), "level=ERROR") || !strings.Contains(out.String(), "error_kind="+kind) || !strings.Contains(out.String(), "level="+level) {
				t.Fatalf("body interruption misclassified: %s", out.String())
			}
		})
	}
}

func TestOIDCKeysBodyClientTimeoutSeverity(t *testing.T) {
	for _, mode := range []string{"headers", "body"} {
		t.Run(mode, func(t *testing.T) {
			out := captureLogs(t, "debug")
			f := newOIDCFixture(t)
			f.mu.Lock()
			f.nonce, f.verifier = "private-nonce", "private-verifier"
			f.mu.Unlock()
			slow := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "body" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer slow.Close()
			s := New(f.config(), nil, &memoryGroups{})
			s.client = f.server.Client()
			s.client.Timeout = 200 * time.Millisecond
			base := s.client.Transport
			s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/keys" {
					r = r.Clone(r.Context())
					r.URL.Host = strings.TrimPrefix(slow.URL, "https://")
				}
				return base.RoundTrip(r)
			})
			_, err := s.oidcIdentity(t.Context(), "code", "private-nonce", "private-verifier")
			if err == nil {
				t.Fatal("interrupted keys response accepted")
			}
			logAuthFailure(httptest.NewRequest("GET", "/auth/campus/callback", nil), "campus", "identity_exchange", "failed", err)
			if strings.Contains(out.String(), "level=ERROR") || !strings.Contains(out.String(), "error_kind=timeout") || !strings.Contains(out.String(), "stage=oidc_verify") || !strings.Contains(out.String(), "outcome=timeout") {
				t.Fatalf("keys body timeout misclassified: %s", out.String())
			}
		})
	}
}

func TestOIDCObservabilityValidationReasonsAndPrivateValues(t *testing.T) {
	f := newOIDCFixture(t)
	s := New(f.config(), nil, &memoryGroups{})
	s.client = f.server.Client()
	for _, mode := range []string{"success", "nonce", "audience", "expiry", "signature", "subject"} {
		t.Run(mode, func(t *testing.T) {
			ctx, output := authLogContext()
			f.mu.Lock()
			f.nonce, f.verifier, f.subject, f.audience = "private-nonce", "private-verifier", "private-subject", "client"
			f.expired, f.invalidSignature = false, false
			reason := ""
			switch mode {
			case "nonce":
				f.nonce, reason = "private-wrong-nonce", "nonce_mismatch"
			case "audience":
				f.audience, reason = "private-wrong-audience", "audience_mismatch"
			case "expiry":
				f.expired, reason = true, "token_expired"
			case "signature":
				f.invalidSignature, reason = true, "signature_invalid"
			case "subject":
				f.subject, reason = "", "subject_missing"
			}
			f.mu.Unlock()
			// The fixture verifies the wire code separately from log redaction.
			_, err := s.oidcIdentity(ctx, "code", "private-nonce", "private-verifier")
			if (err == nil) != (mode == "success") {
				t.Fatalf("unexpected result: %v", err)
			}
			requireAuthLog(t, output, "auth.provider.completed", "discovery", "")
			requireAuthLog(t, output, "auth.provider.completed", "token", "")
			if reason != "" {
				requireAuthLog(t, output, "auth.provider.completed", "verify", reason)
			} else {
				requireAuthLog(t, output, "auth.provider.completed", "claims", "")
			}
			for _, secret := range []string{"private-nonce", "private-wrong-nonce", "private-verifier", "private-subject", "private-wrong-audience", "校园用户", `"access"`} {
				if strings.Contains(output.String(), secret) || err != nil && strings.Contains(err.Error(), secret) {
					t.Errorf("private value %q leaked: %s; error=%v", secret, output, err)
				}
			}
		})
	}
}

func TestPostgresSessionObservability(t *testing.T) {
	db := authTestDB(t)
	userID, cookie := authSession(t, db)
	s := New(Config{}, db, &memoryGroups{})
	for _, mode := range []string{"anonymous", "hit", "expired", "unknown", "database_failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx, output := authLogContext()
			r := httptest.NewRequest("GET", "/api/me", nil).WithContext(ctx)
			reason := "cookie_missing"
			switch mode {
			case "hit":
				r.AddCookie(cookie)
				reason = "session_valid"
			case "expired":
				if _, err := db.Exec(ctx, `UPDATE sessions SET expires_at=now()-interval '1 second'`); err != nil {
					t.Fatal(err)
				}
				r.AddCookie(cookie)
				reason = "session_expired"
			case "unknown":
				r.AddCookie(&http.Cookie{Name: "nailist_session", Value: "private-unknown-cookie"})
				reason = "session_unknown"
			case "database_failure":
				if _, err := db.Exec(ctx, `ALTER TABLE sessions RENAME TO sessions_unavailable`); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _, _ = db.Exec(context.Background(), `ALTER TABLE sessions_unavailable RENAME TO sessions`) })
				r.AddCookie(cookie)
				reason = "database_read_failed"
			}
			authenticated := false
			logRequest(s.session(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, authenticated = r.Context().Value(userKey).(User) }))).ServeHTTP(httptest.NewRecorder(), r)
			if authenticated != (mode == "hit") {
				t.Fatalf("authenticated=%v mode=%s", authenticated, mode)
			}
			requireAuthLog(t, output, "auth.session.lookup", "", reason)
			requireAuthLog(t, output, "http.request.completed", "", "")
			for _, line := range strings.Split(output.String(), "\n") {
				var record map[string]any
				if json.Unmarshal([]byte(line), &record) == nil && record["event"] == "http.request.completed" {
					if mode == "hit" && record["user_id"] != float64(userID) {
						t.Fatalf("summary lost authenticated user: %s", line)
					}
					if mode != "hit" && record["user_id"] != nil {
						t.Fatalf("anonymous summary claims a user: %s", line)
					}
				}
			}
			if strings.Contains(output.String(), cookie.Value) || strings.Contains(output.String(), "private-unknown-cookie") {
				t.Fatal(output.String())
			}
		})
	}
}

func TestPostgresSessionDependencyFailureLevels(t *testing.T) {
	db := authTestDB(t)
	_, cookie := authSession(t, db)
	s := New(Config{}, db, &memoryGroups{})
	for _, mode := range []string{"canceled", "timeout", "database"} {
		t.Run(mode, func(t *testing.T) {
			ctx, output := authLogContext()
			level, outcome, reason := "ERROR", "failed", "database_read_failed"
			switch mode {
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				level, outcome, reason = "INFO", "canceled", "client_canceled"
			case "timeout":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
				level, outcome, reason = "WARN", "timeout", "deadline_exceeded"
			case "database":
				if _, err := db.Exec(ctx, `ALTER TABLE sessions RENAME TO sessions_unavailable`); err != nil {
					t.Fatal(err)
				}
			}
			r := httptest.NewRequest("GET", "/api/me", nil).WithContext(ctx)
			r.AddCookie(cookie)
			logRequest(s.session(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := r.Context().Value(userKey).(User); ok {
					t.Fatal("failed session lookup authenticated request")
				}
				state := r.Context().Value(requestStateKey{}).(*requestState)
				state.mu.Lock()
				errorLogged := state.errorLogged
				state.mu.Unlock()
				if errorLogged != (mode == "database") {
					t.Errorf("error ownership marker=%v for %s", errorLogged, mode)
				}
				w.WriteHeader(http.StatusNoContent)
			}))).ServeHTTP(httptest.NewRecorder(), r)
			record := authLogRecord(t, output, "auth.session.lookup")
			if record["level"] != level || record["outcome"] != outcome || record["reason"] != reason {
				t.Fatal(output.String())
			}
			if mode != "database" && strings.Contains(output.String(), `"level":"ERROR"`) {
				t.Fatal(output.String())
			}
			if strings.Contains(output.String(), cookie.Value) {
				t.Fatal(output.String())
			}
		})
	}
}

func TestAuthDependencyFailureLevelsAndStage(t *testing.T) {
	for _, mode := range []string{"canceled", "timeout", "rejected"} {
		t.Run(mode, func(t *testing.T) {
			ctx, output := authLogContext()
			cause := context.Canceled
			level, outcome, reason := "INFO", "canceled", "client_canceled"
			providerError := &authProviderError{stage: "token", reason: "token_exchange_failed", cause: cause}
			if mode == "timeout" {
				providerError.cause = context.DeadlineExceeded
				level, outcome, reason = "WARN", "timeout", "deadline_exceeded"
			}
			if mode == "rejected" {
				providerError.stage, providerError.reason, providerError.cause, providerError.rejected = "verify", "nonce_mismatch", nil, true
				level, outcome, reason = "WARN", "rejected", "nonce_mismatch"
			}
			r := httptest.NewRequest("GET", "/auth/campus/callback?code=private-code&state=private-state", nil).WithContext(ctx)
			logAuthFailure(r, "campus", "identity_exchange", "failed", providerError)
			record := authLogRecord(t, output, "auth.failed")
			if record["level"] != level || record["outcome"] != outcome || record["reason"] != reason || record["stage"] != "oidc_"+providerError.stage {
				t.Fatal(output.String())
			}
			if strings.Contains(output.String(), "private-code") || strings.Contains(output.String(), "private-state") {
				t.Fatal(output.String())
			}
		})
	}
}

func TestAuthDependencyFailureStepReasons(t *testing.T) {
	for _, tc := range []struct{ stage, publicReason, reason string }{
		{"auth_flow_consume", "failed", "database_read_failed"},
		{"binding_session_recheck", "session_changed", "database_read_failed"},
		{"auth_flow_create", "failed", "database_write_failed"},
		{"session_create", "failed", "database_write_failed"},
		{"account_resolve", "failed", "account_resolution_failed"},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			ctx, output := authLogContext()
			r := httptest.NewRequest("GET", "/auth/qq/callback", nil).WithContext(ctx)
			logAuthFailure(r, "qq", tc.stage, tc.publicReason, errors.New("dependency unavailable"))
			record := authLogRecord(t, output, "auth.failed")
			if record["stage"] != tc.stage || record["reason"] != tc.reason || record["level"] != "ERROR" {
				t.Fatal(output.String())
			}
		})
	}
}

func TestPostgresAuthAPIDependencyFailureSteps(t *testing.T) {
	db := authTestDB(t)
	userID, cookie := authSession(t, db)
	s := New(Config{QQAppID: "app", QQAppKey: "key"}, db, &memoryGroups{})
	if _, err := db.Exec(context.Background(), `ALTER TABLE identities RENAME TO identities_unavailable; ALTER TABLE sessions RENAME TO sessions_unavailable`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, path, event, stage, reason string
		handler                                  http.HandlerFunc
	}{
		{"bind", "POST", "/api/auth/bind/qq", "auth.binding.lookup.failed", "binding_lookup", "database_read_failed", s.bind},
		{"me", "GET", "/api/me", "account.identities.failed", "identities_list", "database_read_failed", s.me},
		{"logout", "POST", "/api/auth/logout", "auth.logout.failed", "session_revoke", "database_write_failed", s.logout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, output := authLogContext()
			ctx = context.WithValue(ctx, userKey, User{ID: userID})
			route := chi.NewRouteContext()
			route.URLParams.Add("provider", "qq")
			ctx = context.WithValue(ctx, chi.RouteCtxKey, route)
			r := httptest.NewRequest(tc.method, tc.path, nil).WithContext(ctx)
			r.AddCookie(cookie)
			out := httptest.NewRecorder()
			logRequest(tc.handler).ServeHTTP(out, r)
			record := authLogRecord(t, output, tc.event)
			if out.Code != http.StatusInternalServerError || record["stage"] != tc.stage || record["reason"] != tc.reason || record["level"] != "ERROR" {
				t.Fatal(out.Code, output.String())
			}
			if strings.Count(output.String(), `"level":"ERROR"`) != 1 || strings.Contains(output.String(), cookie.Value) {
				t.Fatal(output.String())
			}
		})
	}
}

func authLogRecord(t *testing.T, output *bytes.Buffer, event string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(output.String(), "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) == nil && record["event"] == event {
			return record
		}
	}
	t.Fatalf("event %s absent from %s", event, output)
	return nil
}

func TestOIDCDependencyFailureDoesNotLogResponseBody(t *testing.T) {
	for _, mode := range []string{"discovery", "token", "nonstandard_error"} {
		t.Run(mode, func(t *testing.T) {
			var output bytes.Buffer
			logger, err := logging.New(&output, "debug")
			if err != nil {
				t.Fatal(err)
			}
			ctx := logging.WithLogger(context.Background(), logger)
			var upstream *httptest.Server
			upstream = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/.well-known/openid-configuration" && mode != "discovery" {
					_ = json.NewEncoder(w).Encode(map[string]any{"issuer": upstream.URL, "authorization_endpoint": upstream.URL + "/authorize", "token_endpoint": upstream.URL + "/token", "jwks_uri": upstream.URL + "/keys"})
					return
				}
				w.WriteHeader(502)
				errorCode := "server_error"
				if mode == "nonstandard_error" {
					errorCode = "private-response-error-code"
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"error": errorCode, "error_description": "private-response-nickname private-response-subject private-response-nonce"})
			}))
			defer upstream.Close()
			s := New(Config{PublicURL: "https://groups.example", OIDC: OIDCConfig{ID: "campus", Name: "校园", Issuer: upstream.URL, ClientID: "client"}}, nil, &memoryGroups{})
			s.client = upstream.Client()
			_, identityErr := s.oidcIdentity(ctx, "private-auth-code", "private-auth-nonce", "private-auth-verifier")
			if identityErr == nil {
				t.Fatal("provider failure accepted")
			}
			logAuthFailure(httptest.NewRequest("GET", "/auth/campus/callback", nil).WithContext(ctx), "campus", "identity_exchange", "failed", identityErr)
			if !strings.Contains(output.String(), "502") {
				t.Fatalf("failure lost upstream status: %s", &output)
			}
			for _, secret := range []string{"private-response-nickname", "private-response-subject", "private-response-nonce", "private-auth-code", "private-auth-nonce", "private-auth-verifier", "private-response-error-code"} {
				if strings.Contains(output.String(), secret) {
					t.Errorf("upstream failure leaked %q: %s", secret, &output)
				}
			}
		})
	}
}

func TestPostgresAuthRejectionNeverReportsLoginSuccess(t *testing.T) {
	for _, mode := range []string{"state_mismatch", "flow_expired", "provider_rejected", "session_create_failed", "success"} {
		t.Run(mode, func(t *testing.T) {
			db := authTestDB(t)
			ctx, output := authLogContext()
			s := New(Config{PublicURL: "https://groups.example", QQAppID: "app", QQAppKey: "private-client-secret", QQUseUnionID: true}, db, &memoryGroups{})
			calls := 0
			s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				body := `{"access_token":"private-access-token"}`
				if mode == "provider_rejected" {
					body = `{"error":100013,"error_description":"private-response"}`
				}
				if r.URL.Path == "/oauth2.0/me" {
					body = `{"openid":"private-openid","unionid":"private-unionid","client_id":"app"}`
				}
				if r.URL.Path == "/user/get_user_info" {
					body = `{"ret":0,"nickname":"private-nickname"}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
			})}
			start := httptest.NewRecorder()
			s.beginAuth(start, httptest.NewRequest("GET", "/auth/qq", nil).WithContext(ctx), "qq", 0)
			location, err := url.Parse(start.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			state := location.Query().Get("state")
			output.Reset()
			cookieState := state
			if mode == "state_mismatch" {
				cookieState = "private-mismatched-state"
			}
			if mode == "flow_expired" {
				if _, err := db.Exec(ctx, `UPDATE auth_flows SET expires_at=now()-interval '1 second'`); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "session_create_failed" {
				if _, err := db.Exec(ctx, `ALTER TABLE sessions RENAME TO unavailable_sessions`); err != nil {
					t.Fatal(err)
				}
			}
			r := httptest.NewRequest("GET", "/auth/qq/callback?state="+state+"&code=private-auth-code", nil).WithContext(ctx)
			r.AddCookie(&http.Cookie{Name: "qq_state", Value: cookieState})
			out := httptest.NewRecorder()
			s.completeAuth(out, r, "qq")
			if mode != "success" {
				if !strings.Contains(out.Header().Get("Location"), "auth_error=") || strings.Contains(output.String(), `"event":"auth.login.completed"`) {
					t.Fatal(out.Header(), output.String())
				}
				for _, cookie := range out.Result().Cookies() {
					if cookie.Name == "nailist_session" && cookie.Value != "" {
						t.Fatal("failed auth issued session")
					}
				}
			} else {
				if out.Header().Get("Location") != "/" {
					t.Fatal(out.Header(), output.String())
				}
				requireAuthLog(t, output, "auth.login.completed", "complete", "logged_in")
			}
			if mode == "state_mismatch" || mode == "flow_expired" {
				if calls != 0 {
					t.Fatal("rejected authorization exchanged code")
				}
			}
			for _, secret := range []string{state, "private-mismatched-state", "private-auth-code", "private-client-secret", "private-access-token", "private-openid", "private-unionid", "private-nickname", "private-response"} {
				if secret != "" && strings.Contains(output.String(), secret) {
					t.Errorf("private value %q leaked: %s", secret, output)
				}
			}
		})
	}
}
