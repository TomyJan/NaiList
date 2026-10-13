package server

import (
	"context"
	"encoding/json"
	"fmt"
	"nailist/internal/groups"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostgresSitesLifecycle(t *testing.T) {
	db := authTestDB(t)
	ctx := context.Background()
	createUser := func() (int64, string) {
		t.Helper()
		var id int64
		if err := db.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('site owner') RETURNING id`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		token := randomToken()
		if _, err := db.Exec(ctx, `INSERT INTO sessions VALUES($1,$2,'csrf',now()+interval '1 hour')`, hashToken(token), id); err != nil {
			t.Fatal(err)
		}
		return id, token
	}
	actor, token := createUser()
	owner, other := createUser()
	h := New(Config{PublicURL: "http://localhost:8080", AdminUserIDs: map[int64]bool{actor: true}}, db, groups.Store{DB: db}).Routes()
	request := func(method, path, body, auth, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth != "" {
			r.AddCookie(&http.Cookie{Name: "nailist_session", Value: auth})
		}
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
	body := `{"name":"站点","domain":" WWW.Example.COM ","organization":"学校"}`
	expect(request("GET", "/api/sites", "", "", ""), 200)
	for _, path := range []string{"/api/my/sites", "/api/admin/sites"} {
		expect(request("GET", path, "", "", ""), 401)
	}
	expect(request("GET", "/api/admin/sites", "", other, ""), 403)
	expect(request("POST", "/api/sites", body, "", "csrf"), 401)
	expect(request("POST", "/api/sites", body, token, ""), 403)
	w := request("POST", "/api/sites", body, token, "csrf")
	expect(w, 201)
	var saved struct {
		ID     int64  `json:"id"`
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || saved.Domain != "example.com" {
		t.Fatalf("save: %s %v", w.Body.String(), err)
	}
	if strings.Contains(w.Body.String(), "ownerId") {
		t.Fatal("public owner exposed")
	}
	path := fmt.Sprintf("/api/sites/%d", saved.ID)
	expect(request("PUT", path, body, other, "csrf"), 404)
	expect(request("DELETE", path, "", other, "csrf"), 404)
	w = request("POST", "/api/sites", body, other, "csrf")
	expect(w, 409)
	if !strings.Contains(w.Body.String(), "此域名已存在, 请前往首页搜索查看") {
		t.Fatal(w.Body.String())
	}
	for _, invalid := range []string{`{"name":"x","domain":"https://example.com","organization":"x"}`, body + ` {}`, `{"name":"x","domain":"example.com","organization":"x","extra":1}`} {
		expect(request("PUT", path, invalid, token, "csrf"), 400)
	}
	expect(request("PUT", path, `{"name":"新站点","domain":"new.example.com","organization":"学校"}`, token, "csrf"), 200)
	w = request("GET", "/api/my/sites", "", other, "")
	expect(w, 200)
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal(w.Body.String())
	}
	adminPath := fmt.Sprintf("/api/admin/sites/%d", saved.ID)
	adminBody := fmt.Sprintf(`{"name":"转移","domain":"new.example.com","organization":"单位","ownerId":%d}`, owner)
	expect(request("PUT", adminPath, adminBody, token, ""), 403)
	expect(request("PUT", adminPath, strings.Replace(adminBody, fmt.Sprintf(`"ownerId":%d`, owner), `"ownerId":999999`, 1), token, "csrf"), 404)
	w = request("PUT", adminPath, adminBody, token, "csrf")
	expect(w, 200)
	if !strings.Contains(w.Body.String(), fmt.Sprintf(`"ownerId":%d`, owner)) {
		t.Fatal(w.Body.String())
	}
	expect(request("PUT", path, body, token, "csrf"), 404)
	expect(request("DELETE", fmt.Sprintf("/api/admin/users/%d", owner), "", token, "csrf"), 409)
	w = request("GET", "/api/admin/users", "", token, "")
	expect(w, 200)
	if !strings.Contains(w.Body.String(), `"siteCount":1`) {
		t.Fatal(w.Body.String())
	}
	w = request("GET", "/api/admin/stats", "", token, "")
	expect(w, 200)
	if !strings.Contains(w.Body.String(), `"sites":1`) {
		t.Fatal(w.Body.String())
	}
	expect(request("DELETE", path, "", other, "csrf"), 204)
	expect(request("DELETE", path, "", other, "csrf"), 404)
	expect(request("POST", "/api/sites", strings.Replace(body, " WWW.Example.COM ", "new.example.com", 1), token, "csrf"), 201)
	expect(request("DELETE", fmt.Sprintf("/api/admin/users/%d", owner), "", token, "csrf"), 204)
	var history int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM sites WHERE owner_id=$1`, owner).Scan(&history); err != nil || history != 0 {
		t.Fatalf("history: %d %v", history, err)
	}
	chineseBody := `{"name":"甘肃奶龙大学","domain":"甘肃奶龙大学.com","organization":"兰州理工大学"}`
	w = request("POST", "/api/sites", chineseBody, token, "csrf")
	expect(w, 201)
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || saved.Domain != "xn--psszg58cy52cu6lj57b.com" {
		t.Fatalf("Chinese domain: %s %v", w.Body.String(), err)
	}
	expect(request("POST", "/api/sites", strings.Replace(chineseBody, "甘肃奶龙大学.com", saved.Domain, 1), token, "csrf"), 409)
	chinesePath := fmt.Sprintf("/api/sites/%d", saved.ID)
	expect(request("PUT", chinesePath, chineseBody, token, "csrf"), 200)
	w = request("POST", "/api/sites", strings.Replace(chineseBody, "甘肃奶龙大学.com", "other.example.com", 1), token, "csrf")
	expect(w, 201)
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	expect(request("PUT", fmt.Sprintf("/api/sites/%d", saved.ID), chineseBody, token, "csrf"), 409)
	adminConflict := strings.TrimSuffix(chineseBody, "}") + fmt.Sprintf(`,"ownerId":%d}`, actor)
	w = request("PUT", fmt.Sprintf("/api/admin/sites/%d", saved.ID), adminConflict, token, "csrf")
	expect(w, 409)
	if !strings.Contains(w.Body.String(), "此域名已存在, 请前往首页搜索查看") {
		t.Fatal(w.Body.String())
	}
}

func TestPostgresSiteDomainUniquenessAndSoftDelete(t *testing.T) {
	db := authTestDB(t)
	ctx := context.Background()
	var owner int64
	if err := db.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('owner') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.QueryRow(ctx, `INSERT INTO sites(owner_id,name,domain,organization) VALUES($1,'x','example.com','x') RETURNING id`, owner).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO sites(owner_id,name,domain,organization) VALUES($1,'x','EXAMPLE.COM','x')`, owner); err == nil {
		t.Fatal("case-insensitive duplicate accepted")
	}
	if _, err := db.Exec(ctx, `UPDATE sites SET deleted_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO sites(owner_id,name,domain,organization) VALUES($1,'x','example.com','x')`, owner); err != nil {
		t.Fatalf("soft deletion did not release domain: %v", err)
	}
}
