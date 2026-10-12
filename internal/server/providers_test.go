package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"github.com/go-jose/go-jose/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"nailist/migrations"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type oidcFixture struct {
	mu                                 sync.Mutex
	server                             *httptest.Server
	nonce, verifier, audience, subject string
	expired, invalidSignature          bool
	exchanges                          int
}

func (f *oidcFixture) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.exchanges }
func (f *oidcFixture) flow(t *testing.T, db *pgxpool.Pool, destination *url.URL) {
	t.Helper()
	var verifier string
	if err := db.QueryRow(context.Background(), `SELECT verifier FROM auth_flows WHERE state_hash=$1`, hashToken(destination.Query().Get("state"))).Scan(&verifier); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nonce = destination.Query().Get("nonce")
	f.verifier = verifier
}
func newOIDCFixture(t *testing.T) *oidcFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &oidcFixture{audience: "client", subject: "oidc-user"}
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		issuer := f.server.URL
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			f.exchanges++
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			if r.Form.Get("code_verifier") != f.verifier || r.Form.Get("code") != "code" || r.Form.Get("redirect_uri") != "https://groups.example/auth/campus/callback" {
				t.Error("missing code, PKCE verifier or callback")
			}
			signingKey := key
			if f.invalidSignature {
				signingKey = wrongKey
			}
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: signingKey}, (&jose.SignerOptions{}).WithHeader("kid", "test"))
			if err != nil {
				t.Error(err)
				http.Error(w, "sign", 500)
				return
			}
			expiry := time.Now().Add(time.Hour)
			if f.expired {
				expiry = time.Now().Add(-time.Hour)
			}
			claims := map[string]any{"iss": issuer, "aud": f.audience, "sub": f.subject, "exp": expiry.Unix(), "iat": time.Now().Unix(), "nonce": f.nonce, "name": "校园用户", "picture": "https://id.example/avatar.png"}
			data, _ := json.Marshal(claims)
			signed, err := signer.Sign(data)
			if err != nil {
				t.Error(err)
				return
			}
			raw, err := signed.CompactSerialize()
			if err != nil {
				t.Error(err)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "token_type": "Bearer", "expires_in": 3600, "id_token": raw})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}
func (f *oidcFixture) config() Config {
	return Config{PublicURL: "https://groups.example", OIDC: OIDCConfig{ID: "campus", Name: "校园", Icon: "https://id.example/icon.png", Issuer: f.server.URL, ClientID: "client", ClientSecret: "secret"}}
}

func TestOIDCVerifiedIdentityAndPKCE(t *testing.T) {
	f := newOIDCFixture(t)
	s := New(f.config(), nil, &memoryGroups{})
	s.client = f.server.Client()
	f.mu.Lock()
	f.nonce = "nonce"
	f.verifier = "verifier"
	f.mu.Unlock()
	identity, err := s.oidcIdentity(context.Background(), "code", "nonce", "verifier")
	if err != nil || identity.Subject != "oidc-user" || identity.Nickname != "校园用户" {
		t.Fatalf("identity: %#v %v", identity, err)
	}
	for _, mode := range []string{"nonce", "audience", "expiry", "signature", "subject"} {
		t.Run(mode, func(t *testing.T) {
			f.mu.Lock()
			f.nonce = "nonce"
			f.audience = "client"
			f.expired = false
			f.invalidSignature = false
			f.subject = "oidc-user"
			switch mode {
			case "nonce":
				f.nonce = "wrong"
			case "audience":
				f.audience = "wrong"
			case "expiry":
				f.expired = true
			case "signature":
				f.invalidSignature = true
			case "subject":
				f.subject = ""
			}
			f.mu.Unlock()
			if _, err := s.oidcIdentity(context.Background(), "code", "nonce", "verifier"); err == nil {
				t.Fatal("invalid identity accepted")
			}
		})
	}
}

func authTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("auth_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close() })
	if _, err = db.Exec(ctx, migrations.SQL); err != nil {
		t.Fatal(err)
	}
	return db
}
func authSession(t *testing.T, db *pgxpool.Pool) (int64, *http.Cookie) {
	t.Helper()
	var id int64
	ctx := context.Background()
	if err := db.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('原账号') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	token := randomToken()
	if _, err := db.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,'csrf',now()+interval '1 hour')`, hashToken(token), id); err != nil {
		t.Fatal(err)
	}
	return id, &http.Cookie{Name: "nailist_session", Value: token}
}
func TestPostgresOIDCBindingFlowAndReplay(t *testing.T) {
	db := authTestDB(t)
	f := newOIDCFixture(t)
	s := New(f.config(), db, &memoryGroups{})
	s.client = f.server.Client()
	handler := s.Routes()
	id, session := authSession(t, db)
	anonymous := httptest.NewRecorder()
	handler.ServeHTTP(anonymous, httptest.NewRequest("POST", "/api/auth/bind/campus", nil))
	if anonymous.Code != 401 {
		t.Fatal("anonymous binding allowed")
	}
	unsafe := httptest.NewRequest("POST", "/api/auth/bind/campus", nil)
	unsafe.AddCookie(session)
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, unsafe)
	if rejected.Code != 403 {
		t.Fatal("binding without CSRF allowed")
	}
	start := func(cookie *http.Cookie) (*httptest.ResponseRecorder, *url.URL) {
		req := httptest.NewRequest("POST", "/api/auth/bind/campus", nil)
		req.AddCookie(cookie)
		req.Header.Set("X-CSRF-Token", "csrf")
		req.Header.Set("Origin", "https://groups.example")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		var result struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(out.Body.Bytes(), &result)
		destination, _ := url.Parse(result.URL)
		return out, destination
	}
	out, destination := start(session)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	state := destination.Query().Get("state")
	if destination.Query().Get("code_challenge_method") != "S256" || destination.Query().Get("code_challenge") == "" {
		t.Fatal("missing PKCE")
	}
	f.flow(t, db, destination)
	callback := func(cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/auth/campus/callback?state="+state+"&code=code", nil)
		req.AddCookie(&http.Cookie{Name: "oidc_state", Value: state})
		req.AddCookie(cookie)
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		return result
	}
	out = callback(session)
	if out.Header().Get("Location") != "/manage?bound=1" {
		t.Fatal("binding failed", out.Header().Get("Location"))
	}
	var boundID int64
	if err := db.QueryRow(context.Background(), `SELECT user_id FROM identities WHERE provider_id='campus'`).Scan(&boundID); err != nil || boundID != id {
		t.Fatal("wrong account bound", err)
	}
	if out = callback(session); !strings.Contains(out.Header().Get("Location"), "auth_error") {
		t.Fatal("replay accepted")
	}
	if f.count() != 1 {
		t.Fatal("replayed code exchanged")
	}
	out, _ = start(session)
	if out.Code != 409 {
		t.Fatal("already bound flow allowed", out.Code)
	}
	// Another account cannot steal the bound identity.
	_, other := authSession(t, db)
	out, destination = start(other)
	state = destination.Query().Get("state")
	f.flow(t, db, destination)
	out = callback(other)
	if out.Header().Get("Location") != "/manage?auth_error=identity_conflict" {
		t.Fatal("cross-account binding accepted", out.Header().Get("Location"))
	}
	// A changed or missing initiating session invalidates the flow before token exchange.
	out, destination = start(other)
	state = destination.Query().Get("state")
	before := f.count()
	out = callback(session)
	if out.Header().Get("Location") != "/manage?auth_error=session_changed" || f.count() != before {
		t.Fatal("changed session accepted")
	}
	out, destination = start(other)
	state = destination.Query().Get("state")
	if _, err := db.Exec(context.Background(), `UPDATE auth_flows SET expires_at=now()-interval '1 second' WHERE state_hash=$1`, hashToken(state)); err != nil {
		t.Fatal(err)
	}
	out = callback(other)
	if !strings.Contains(out.Header().Get("Location"), "auth_error") || f.count() != before {
		t.Fatal("expired flow accepted")
	}
	// Login with the bound provider resolves to the original account.
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest("GET", "/auth/campus", nil))
	destination, _ = url.Parse(login.Header().Get("Location"))
	state = destination.Query().Get("state")
	f.flow(t, db, destination)
	out = callback(other)
	if out.Header().Get("Location") != "/" {
		t.Fatal("OIDC login failed", out.Header().Get("Location"))
	}
	for _, cookie := range out.Result().Cookies() {
		if cookie.Name == "nailist_session" {
			var account int64
			err := db.QueryRow(context.Background(), `SELECT user_id FROM sessions WHERE token_hash=$1`, hashToken(cookie.Value)).Scan(&account)
			if err != nil || account != id {
				t.Fatal("OIDC login selected wrong account", err)
			}
		}
	}
	me := httptest.NewRequest("GET", "/api/me", nil)
	me.AddCookie(session)
	out = httptest.NewRecorder()
	handler.ServeHTTP(out, me)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "campus") || strings.Contains(out.Body.String(), "oidc-user") || strings.Contains(out.Body.String(), "issuer") {
		t.Fatal("binding information leaked private identity", out.Body.String())
	}
	unlink := httptest.NewRequest("DELETE", "/api/auth/bind/campus", nil)
	unlink.AddCookie(session)
	out = httptest.NewRecorder()
	handler.ServeHTTP(out, unlink)
	if out.Code < 400 {
		t.Fatal("unlink endpoint exposed")
	}
}

func TestQQUnionIDConfiguration(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			s := New(Config{QQAppID: "app", QQAppKey: "key", QQUseUnionID: enabled}, nil, &memoryGroups{})
			s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := `{"access_token":"access"}`
				if r.URL.Path == "/oauth2.0/me" {
					if (r.URL.Query().Get("unionid") == "1") != enabled {
						t.Fatal("wrong unionid request")
					}
					body = `{"openid":"open","client_id":"app","unionid":"union"}`
				}
				if r.URL.Path == "/user/get_user_info" {
					body = `{"ret":0,"nickname":"用户"}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
			})}
			identity, err := s.qqIdentity(context.Background(), "code")
			if err != nil || identity.Subject != "open" {
				t.Fatal(err)
			}
		})
	}
}
func TestProviderConfigurationAndNoSecrets(t *testing.T) {
	cfg := Config{QQAppID: "app", QQAppKey: "qq-secret", OIDC: OIDCConfig{ID: "campus", Name: "校园", Issuer: "https://id.example", ClientID: "client", ClientSecret: "oidc-secret"}}
	if err := ValidateAuthConfig(cfg); err != nil {
		t.Fatal(err)
	}
	s := New(cfg, nil, &memoryGroups{})
	out := httptest.NewRecorder()
	s.Routes().ServeHTTP(out, httptest.NewRequest("GET", "/api/auth/providers", nil))
	if strings.Contains(out.Body.String(), "secret") || !strings.Contains(out.Body.String(), "campus") {
		t.Fatal("providers response", out.Body.String())
	}
	for _, id := range []string{"qq", "../evil", "UpperCase"} {
		bad := cfg
		bad.OIDC.ID = id
		if ValidateAuthConfig(bad) == nil {
			t.Fatal("invalid provider ID accepted")
		}
	}
	cfg.OIDC.Icon = "//evil.example/icon.png"
	if ValidateAuthConfig(cfg) == nil {
		t.Fatal("unsafe icon accepted")
	}
}
