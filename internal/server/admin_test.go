package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"nailist/internal/groups"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestPostgresOrdinaryGroupCreateAfterAccountDeletion(t *testing.T) {
	db := authTestDB(t)
	_, err := (groups.Store{DB: db}).Save(context.Background(), 999999, 0, groups.Group{Organization: "test", QQNumber: "12345", JoinURL: "https://qm.qq.com/q/test", Keywords: []string{}})
	if !errors.Is(err, groups.ErrNotFound) {
		t.Fatalf("deleted owner must be expected not-found rejection: %v", err)
	}
}

func TestAdminAuthenticationAndAuthorization(t *testing.T) {
	db := authTestDB(t)
	var id int64
	if err := db.QueryRow(context.Background(), `INSERT INTO users(nickname) VALUES('ordinary') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	token := randomToken()
	if _, err := db.Exec(context.Background(), `INSERT INTO sessions VALUES($1,$2,'csrf',now()+interval '1 hour')`, hashToken(token), id); err != nil {
		t.Fatal(err)
	}
	h := New(Config{PublicURL: "http://localhost:8080"}, db, groups.Store{DB: db}).Routes()
	for _, tc := range []struct {
		token  string
		status int
	}{{"", 401}, {token, 403}} {
		r := httptest.NewRequest("GET", "/api/admin/users", nil)
		if tc.token != "" {
			r.AddCookie(&http.Cookie{Name: "nailist_session", Value: tc.token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("admin authorization: got %d want %d: %s", w.Code, tc.status, w.Body.String())
		}
	}
}

func TestAdminPostgresManagement(t *testing.T) {
	db := authTestDB(t)
	ctx := context.Background()
	createUser := func(name string) int64 {
		t.Helper()
		var id int64
		if err := db.QueryRow(ctx, `INSERT INTO users(nickname) VALUES($1) RETURNING id`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	adminID, target, owner := createUser("admin"), createUser("target"), createUser("owner")
	session := func(id int64) string {
		t.Helper()
		token := randomToken()
		if _, err := db.Exec(ctx, `INSERT INTO sessions VALUES($1,$2,'csrf',now()+interval '1 hour')`, hashToken(token), id); err != nil {
			t.Fatal(err)
		}
		return token
	}
	token, targetToken := session(adminID), session(target)
	for _, provider := range []string{"qq", "campus"} {
		if _, err := db.Exec(ctx, `INSERT INTO identities(user_id,provider_id,issuer,subject,nickname) VALUES($1,$2,'issuer',$2,'identity')`, target, provider); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `INSERT INTO auth_flows VALUES('pending','campus',$1,'session','nonce','verifier',now()+interval '1 hour')`, target); err != nil {
		t.Fatal(err)
	}
	h := New(Config{PublicURL: "http://localhost:8080", AdminUserIDs: map[int64]bool{adminID: true}}, db, groups.Store{DB: db}).Routes()
	request := func(method, path, body, auth, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "nailist_session", Value: auth})
		r.Header.Set("Origin", "http://localhost:8080")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("got %d want %d: %s", w.Code, status, w.Body.String())
		}
	}
	expect(request("GET", "/api/me", "", token, ""), 200)
	if w := request("GET", "/api/me", "", token, ""); !strings.Contains(w.Body.String(), `"isAdmin":true`) {
		t.Fatal(w.Body.String())
	}
	w := request("GET", "/api/admin/users", "", token, "")
	expect(w, 200)
	if strings.Contains(w.Body.String(), "subject") || strings.Contains(w.Body.String(), "issuer") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Body.String())
	}
	path := fmt.Sprintf("/api/admin/users/%d", target)
	expect(request("DELETE", path+"/identities/qq", "", token, ""), 403)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, provider := range []string{"qq", "campus"} {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			codes <- request("DELETE", path+"/identities/"+p, "", token, "csrf").Code
		}(provider)
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for c := range codes {
		if c == 204 {
			success++
		}
		if c == 409 {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent unbind: success=%d conflict=%d", success, conflict)
	}
	expect(request("GET", "/api/my/groups", "", targetToken, ""), 401)
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM auth_flows WHERE bind_user_id=$1`, target).Scan(&count); err != nil || count != 0 {
		t.Fatalf("pending flow not revoked: %d %v", count, err)
	}
	expect(request("DELETE", path+"/identities/missing", "", token, "csrf"), 404)
	var gid, otherGID int64
	for i, id := range []int64{target, owner} {
		var got int64
		if err := db.QueryRow(ctx, `INSERT INTO groups(owner_id,organization,qq_number,join_url) VALUES($1,'old',$2,'https://qm.qq.com/q/old') RETURNING id`, id, fmt.Sprintf("12345%d", i)).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			gid = got
		} else {
			otherGID = got
		}
	}
	_ = otherGID
	expect(request("DELETE", path, "", token, "csrf"), 409)
	expect(request("DELETE", fmt.Sprintf("/api/admin/users/%d", adminID), "", token, "csrf"), 409)
	groupPath := fmt.Sprintf("/api/admin/groups/%d", gid)
	body := func(ownerID int64, number string) string {
		return fmt.Sprintf(`{"organization":"new","keywords":"a，b,a","qqNumber":%q,"joinUrl":"https://qm.qq.com/q/new","ownerId":%d}`, number, ownerID)
	}
	expect(request("PUT", groupPath, body(999999, "123450"), token, "csrf"), 404)
	expect(request("PUT", groupPath, body(owner, "123451"), token, "csrf"), 409)
	expect(request("PUT", groupPath, body(owner, "bad"), token, "csrf"), 400)
	w = request("PUT", groupPath, body(owner, "123450"), token, "csrf")
	expect(w, 200)
	var saved struct {
		OwnerID      int64    `json:"ownerId"`
		Organization string   `json:"organization"`
		Keywords     []string `json:"keywords"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || saved.OwnerID != owner || saved.Organization != "new" || len(saved.Keywords) != 2 {
		t.Fatalf("atomic save: %s %v", w.Body.String(), err)
	}
	expect(request("GET", "/api/admin/groups", "", token, ""), 200)
	if _, err := db.Exec(ctx, `INSERT INTO groups(owner_id,organization,qq_number,join_url,deleted_at) VALUES($1,'history','999999','https://qm.qq.com/q/history',now())`, target); err != nil {
		t.Fatal(err)
	}
	expect(request("DELETE", path, "", token, "csrf"), 204)
	expect(request("DELETE", path, "", token, "csrf"), 404)
	for _, table := range []string{"users", "groups", "sessions", "identities", "auth_flows"} {
		column := "user_id"
		if table == "users" {
			column = "id"
		}
		if table == "groups" {
			column = "owner_id"
		}
		if table == "auth_flows" {
			column = "bind_user_id"
		}
		if err := db.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE `+column+`=$1`, target).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s leftovers: %d %v", table, count, err)
		}
	}
}
