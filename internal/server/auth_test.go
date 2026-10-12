package server

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"nailist/internal/groups"
	"nailist/migrations"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestQQLoginRedirectAndStateCookie(t *testing.T) {
	s := New(Config{PublicURL: "https://groups.example", QQAppID: "app", QQAppKey: "key"}, nil, &memoryGroups{})
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/auth/qq", nil))
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil || location.Host != "graph.qq.com" {
		t.Fatal("wrong OAuth destination")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].MaxAge != 600 {
		t.Fatal("state cookie unsafe")
	}
	if location.Query().Get("state") != cookies[0].Value || location.Query().Get("redirect_uri") != "https://groups.example/auth/qq/callback" {
		t.Fatal("OAuth params incorrect")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPostgresQQCallbackAndExpiredSession(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(ctx, migrations.SQL); err != nil {
		t.Fatal(err)
	}
	openid := randomToken()
	var accountID int64
	calls := 0
	s := New(Config{PublicURL: "https://groups.example", QQAppID: "app", QQAppKey: "key"}, db, groups.Store{DB: db})
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var body string
		switch r.URL.Path {
		case "/oauth2.0/token":
			if r.URL.Query().Get("code") != "code" {
				t.Fatal("code not exchanged")
			}
			body = `{"access_token":"access"}`
		case "/oauth2.0/me":
			body = `{"client_id":"app","openid":"` + openid + `"}`
		case "/user/get_user_info":
			body = `{"ret":0,"nickname":"QQ 测试用户","figureurl_qq_2":"https://example.com/avatar"}`
		default:
			t.Fatal("unexpected OAuth request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	defer func() {
		_, _ = db.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, accountID)
		_, _ = db.Exec(ctx, `DELETE FROM users WHERE id=$1`, accountID)
	}()
	bad := httptest.NewRequest("GET", "/auth/qq/callback?state=wrong&code=code", nil)
	bad.AddCookie(&http.Cookie{Name: "qq_state", Value: "right"})
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, bad)
	if calls != 0 || !strings.Contains(w.Header().Get("Location"), "auth_error") {
		t.Fatal("bad OAuth state accepted")
	}
	start := httptest.NewRecorder()
	s.Routes().ServeHTTP(start, httptest.NewRequest("GET", "/auth/qq", nil))
	location, _ := url.Parse(start.Header().Get("Location"))
	r := httptest.NewRequest("GET", "/auth/qq/callback?state="+location.Query().Get("state")+"&code=code", nil)
	for _, cookie := range start.Result().Cookies() {
		r.AddCookie(cookie)
	}
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	if w.Code != 303 || w.Header().Get("Location") != "/" || calls != 3 {
		t.Fatalf("callback failed: %d %s", w.Code, w.Header().Get("Location"))
	}
	if err := db.QueryRow(ctx, `SELECT user_id FROM identities WHERE subject=$1`, openid).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	var session *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "nailist_session" {
			session = cookie
		}
	}
	if session == nil || !session.HttpOnly || !session.Secure {
		t.Fatal("missing session")
	}
	me := httptest.NewRequest("GET", "/api/me", nil)
	me.AddCookie(session)
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, me)
	if !strings.Contains(w.Body.String(), "QQ 测试用户") {
		t.Fatal("session not persisted")
	}
	if _, err = db.Exec(ctx, `UPDATE sessions SET expires_at=now()-interval '1 minute' WHERE token_hash=$1`, hashToken(session.Value)); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.Routes().ServeHTTP(w, me)
	if strings.TrimSpace(w.Body.String()) != "null" {
		t.Fatal("expired session accepted")
	}
}
