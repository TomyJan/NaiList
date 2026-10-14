package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nailist/internal/groups"
)

func TestPostgresPublicListCacheLifecycle(t *testing.T) {
	db := securityDatabase(t)
	ctx := context.Background()
	var actor, owner int64
	for _, id := range []*int64{&actor, &owner} {
		if err := db.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('cache') RETURNING id`).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,'csrf',now()+interval '1 hour')`, hashToken("cache-session"), actor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,'csrf',now()-interval '1 hour')`, hashToken("expired-session"), actor); err != nil {
		t.Fatal(err)
	}
	h := New(Config{PublicURL: "https://groups.example", AdminUserIDs: map[int64]bool{actor: true}}, db, groups.Store{DB: db}).Routes()
	request := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "nailist_session", Value: "cache-session"})
			r.Header.Set("X-CSRF-Token", "csrf")
			r.Header.Set("Origin", "https://groups.example")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	read := func(kind string, queries int64, contains string) {
		t.Helper()
		for _, token := range []string{"cache-session", "", "unknown-session", "expired-session"} {
			before := db.Stat().AcquireCount()
			r := httptest.NewRequest(http.MethodGet, "/api/"+kind, nil)
			if token != "" {
				r.AddCookie(&http.Cookie{Name: "nailist_session", Value: token})
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 200 || !strings.Contains(w.Body.String(), contains) {
				t.Fatalf("%s: %d %s", kind, w.Code, w.Body.String())
			}
			if got := db.Stat().AcquireCount() - before; got != queries {
				t.Fatalf("%s (cookie=%t) queried DB %d times, want %d", kind, token != "", got, queries)
			}
			if strings.Contains(w.Body.String(), "ownerId") {
				t.Fatal("public snapshot exposed ownership")
			}
			if w.Header().Get("Set-Cookie") != "" {
				t.Fatal("public list modified session cookies")
			}
			queries = 0
		}
	}
	for _, kind := range []string{"groups", "sites"} {
		read(kind, 1, "[]")
		read(kind, 0, "[]")
	}
	for _, kind := range []string{"groups", "sites"} {
		t.Run(kind, func(t *testing.T) {
			body := `{"organization":"学校","qqNumber":"123456789","joinUrl":"https://qm.qq.com/q/test","keywords":"cache"}`
			if kind == "sites" {
				body = `{"name":"站点","organization":"学校","domain":"cache.example"}`
			}
			mutate := func(method, path, content string, status int) *httptest.ResponseRecorder {
				t.Helper()
				w := request(method, path, content, true)
				if w.Code != status {
					t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
				}
				return w
			}
			w := mutate(http.MethodPost, "/api/"+kind, body, 201)
			var item struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
				t.Fatal(err)
			}
			path := fmt.Sprintf("/api/%s/%d", kind, item.ID)
			read(kind, 1, "学校")
			read(kind, 0, "学校")
			otherKind := "sites"
			if kind == "sites" {
				otherKind = "groups"
			}
			read(otherKind, 0, "[]")
			mutate(http.MethodPost, "/api/"+kind, body, 409)
			mutate(http.MethodPut, path, "{", 400)
			mutate(http.MethodDelete, "/api/"+kind+"/99999999", "", 404)
			read(kind, 0, "学校")
			body = strings.Replace(body, "学校", "新学校", 1)
			mutate(http.MethodPut, path, body, 200)
			read(kind, 1, "新学校")
			read(kind, 0, "新学校")
			// Private reads stay live and do not fill or replace the public snapshot.
			for i := 0; i < 2; i++ {
				before := db.Stat().AcquireCount()
				w := request(http.MethodGet, "/api/my/"+kind, "", true)
				if w.Code != 200 || db.Stat().AcquireCount()-before != 2 {
					t.Fatal("personal list no longer queries the database")
				}
			}
			adminBody := strings.TrimSuffix(strings.Replace(body, "新学校", "管理学校", 1), "}") + fmt.Sprintf(`,"ownerId":%d}`, owner)
			mutate(http.MethodPut, fmt.Sprintf("/api/admin/%s/%d", kind, item.ID), adminBody, 200)
			read(kind, 1, "管理学校")
			read(kind, 0, "管理学校")
			read(otherKind, 0, "[]")
			// Transfer back before deleting through the personal API.
			adminBody = strings.TrimSuffix(body, "}") + fmt.Sprintf(`,"ownerId":%d}`, actor)
			mutate(http.MethodPut, fmt.Sprintf("/api/admin/%s/%d", kind, item.ID), adminBody, 200)
			read(kind, 1, "新学校")
			mutate(http.MethodDelete, path, "", 204)
			read(kind, 1, "[]")
			read(kind, 0, "[]")
		})
	}
}
