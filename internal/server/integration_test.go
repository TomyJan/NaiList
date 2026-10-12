package server

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"nailist/internal/groups"
	"nailist/migrations"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestPostgresCRUDSessionsAndOwnership(t *testing.T) {
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
	if _, err := db.Exec(ctx, migrations.SQL); err != nil {
		t.Fatal(err)
	}
	var owner, other int64
	if err := db.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('test') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('other') RETURNING id`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.Exec(ctx, `DELETE FROM groups WHERE owner_id IN ($1,$2)`, owner, other)
		_, _ = db.Exec(ctx, `DELETE FROM sessions WHERE user_id IN ($1,$2)`, owner, other)
		_, _ = db.Exec(ctx, `DELETE FROM users WHERE id IN ($1,$2)`, owner, other)
	}()
	token, otherToken := randomToken(), randomToken()
	for _, s := range []struct {
		token string
		id    int64
	}{{token, owner}, {otherToken, other}} {
		if _, err := db.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,'csrf',now()+interval '1 hour')`, hashToken(s.token), s.id); err != nil {
			t.Fatal(err)
		}
	}
	server := New(Config{PublicURL: "http://localhost:8080"}, db, groups.Store{DB: db})
	handler := server.Routes()
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "nailist_session", Value: token})
		r.Header.Set("X-CSRF-Token", "csrf")
		r.Header.Set("Origin", "http://localhost:8080")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	number := strconv.FormatInt(900000000+owner, 10)
	body := `{"organization":"兰州理工大学","keywords":"烂梨,兰理，烂🍐","qqNumber":"` + number + `","joinUrl":"https://qm.qq.com/q/test"}`
	w := request("POST", "/api/groups", body, token)
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var g groups.Group
	if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	path := "/api/groups/" + strconv.FormatInt(g.ID, 10)
	if w = request("GET", "/api/groups", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "烂🍐") {
		t.Fatalf("list %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/api/my/groups", "", otherToken); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal("my groups leaked")
	}
	for _, method := range []string{"PUT", "DELETE"} {
		if w = request(method, path, body, otherToken); w.Code != 404 {
			t.Fatalf("ownership %s %d", method, w.Code)
		}
	}
	if w = request("POST", "/api/groups", body, token); w.Code != 409 {
		t.Fatal("duplicate accepted")
	}
	if w = request("PUT", path, strings.Replace(body, "烂梨", "新简称", 1), token); w.Code != 200 {
		t.Fatalf("edit %d", w.Code)
	}
	if w = request("DELETE", path, "", token); w.Code != 204 {
		t.Fatalf("delete %d", w.Code)
	}
	var deleted bool
	if err := db.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM groups WHERE id=$1`, g.ID).Scan(&deleted); err != nil || !deleted {
		t.Fatal("not soft deleted")
	}
	if w = request("GET", "/api/groups", "", ""); strings.Contains(w.Body.String(), number) {
		t.Fatal("deleted group visible")
	}
	if w = request("POST", "/api/auth/logout", "", token); w.Code != 204 {
		t.Fatal("logout failed")
	}
	if w = request("POST", "/api/groups", body, token); w.Code != 401 {
		t.Fatal("session not revoked")
	}
}
