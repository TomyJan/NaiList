package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestPostgresPublicRoutesDoNotReadSession(t *testing.T) {
	db := securityDatabase(t)
	_, session := authSession(t, db)
	h := New(Config{}, db, &memoryGroups{}).Routes()
	for _, tc := range []struct {
		path    string
		status  int
		queries int64
	}{
		{"/", 200, 0},
		{"/favicon.ico", 200, 0},
		{"/api/auth/providers", 200, 0},
		{"/api/traffic", 404, 0}, // Disabled statistics still bypass session lookup.
		{"/api/health", 200, 1},  // DB readiness probe, not a session lookup.
		{"/api/nonexistent", 404, 0},
		{"/api/me", 200, 2}, // Session and identities are both required.
		{"/api/my/groups", 200, 1},
		{"/api/admin/groups", 403, 1}, // Valid session does not grant admin access.
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.path, nil)
			r.AddCookie(session)
			before := db.Stat().AcquireCount()
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if got := db.Stat().AcquireCount() - before; got != tc.queries || w.Code != tc.status {
				t.Fatalf("status=%d queries=%d; want status=%d queries=%d", w.Code, got, tc.status, tc.queries)
			}
		})
	}
}

func TestPostgresLoginDoesNotReadExistingSession(t *testing.T) {
	db := securityDatabase(t)
	_, session := authSession(t, db)
	f := newOIDCFixture(t)
	cfg := f.config()
	cfg.QQAppID, cfg.QQAppKey = "test", "test"
	s := New(cfg, db, &memoryGroups{})
	s.client = f.server.Client()
	h := s.Routes()
	for _, provider := range []string{"qq", "campus"} {
		t.Run(provider, func(t *testing.T) {
			before := db.Stat().AcquireCount()
			r := httptest.NewRequest(http.MethodGet, "/auth/"+provider, nil)
			r.AddCookie(session)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusFound {
				t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
			}
			// Only expiration cleanup and authorization flow creation need the DB.
			if got := db.Stat().AcquireCount() - before; got != 2 {
				t.Errorf("login acquired DB %d times, want 2", got)
			}
			destination, err := url.Parse(w.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			state := destination.Query().Get("state")
			if state == "" {
				t.Fatal("authorization state missing")
			}
			callback := func(value string, queries int64) {
				t.Helper()
				r := httptest.NewRequest(http.MethodGet, "/auth/"+provider+"/callback?state="+value, nil)
				r.AddCookie(session)
				r.AddCookie(&http.Cookie{Name: stateCookie(provider), Value: state})
				before := db.Stat().AcquireCount()
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Header().Get("Location") != "/?auth_error=failed" {
					t.Fatalf("callback accepted missing code or invalid state: %d %s", w.Code, w.Header().Get("Location"))
				}
				if got := db.Stat().AcquireCount() - before; got != queries {
					t.Errorf("callback acquired DB %d times, want %d", got, queries)
				}
			}
			callback("invalid-state", 0)
			callback(state, 1) // Consume flow, then reject the missing code.
			callback(state, 1) // Replay only checks for the already consumed flow.
		})
	}
}
