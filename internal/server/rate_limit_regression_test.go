package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"nailist/internal/groups"
)

func TestCallbackProviderRateBuckets(t *testing.T) {
	for _, start := range []struct{ method, path string }{
		{http.MethodGet, "/auth/callback"},
		{http.MethodPost, "/api/auth/bind/callback"},
	} {
		t.Run(start.path, func(t *testing.T) {
			s := New(Config{}, nil, &memoryGroups{})
			h := s.authRateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
			request := func(method, path string) int {
				r := httptest.NewRequest(method, path, nil)
				r.RemoteAddr = "192.0.2.1:1234"
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w.Code
			}
			for i := 0; i < 20; i++ {
				if got := request(start.method, start.path); got != http.StatusNoContent {
					t.Fatalf("start %d: %d", i, got)
				}
			}
			if got := request(start.method, start.path); got == http.StatusNoContent {
				t.Fatal("start bypassed 20/minute limit")
			}
			for i := 0; i < 60; i++ {
				if got := request(http.MethodGet, "/auth/callback/callback"); got != http.StatusNoContent {
					t.Fatalf("callback %d: %d", i, got)
				}
			}
			if got := request(http.MethodGet, "/auth/callback/callback"); got == http.StatusNoContent {
				t.Fatal("callback bypassed 60/minute limit")
			}
		})
	}
}

func TestLogoutRemainsAvailableAfterWriteLimits(t *testing.T) {
	for _, writes := range []int{30, 200} {
		t.Run(strconv.Itoa(writes), func(t *testing.T) {
			db := securityDatabase(t)
			ctx := context.Background()
			var id int64
			if err := db.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('logout') RETURNING id`).Scan(&id); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,'csrf',now()+interval '1 hour')`, hashToken("logout-session"), id); err != nil {
				t.Fatal(err)
			}
			s := New(Config{PublicURL: "https://groups.example"}, db, groups.Store{DB: db})
			now := time.Now()
			s.limiter.now = func() time.Time { return now }
			h := s.Routes()
			request := func(path, session, csrf string) int {
				r := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{"))
				if session != "" {
					r.AddCookie(&http.Cookie{Name: "nailist_session", Value: session})
				}
				r.Header.Set("X-CSRF-Token", csrf)
				r.Header.Set("Origin", "https://groups.example")
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w.Code
			}
			for i := 0; i < writes; i++ {
				if i > 0 && i%30 == 0 {
					now = now.Add(time.Minute)
				}
				if got := request("/api/groups", "logout-session", "csrf"); got != http.StatusBadRequest {
					t.Fatalf("write %d: %d", i, got)
				}
			}
			if writes == 200 {
				now = now.Add(time.Minute)
			}
			if got := request("/api/groups", "logout-session", "csrf"); got != http.StatusTooManyRequests {
				t.Fatalf("write budget not exhausted: %d", got)
			}
			if got := request("/api/auth/logout", "", ""); got != http.StatusUnauthorized {
				t.Fatalf("unauthenticated logout: %d", got)
			}
			if got := request("/api/auth/logout", "logout-session", ""); got != http.StatusForbidden {
				t.Fatalf("logout without CSRF: %d", got)
			}
			if got := request("/api/auth/logout", "logout-session", "csrf"); got != http.StatusNoContent {
				t.Fatalf("logout blocked by write quota: %d", got)
			}
			var live bool
			if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE user_id=$1)`, id).Scan(&live); err != nil {
				t.Fatal(err)
			}
			if live {
				t.Fatal("logout did not revoke session")
			}
		})
	}
}
