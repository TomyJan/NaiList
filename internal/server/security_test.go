package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"nailist/internal/groups"
	"nailist/internal/resources"
	"nailist/internal/sites"
	"nailist/migrations"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuthStartRateLimitAndCallbackIsolation(t *testing.T) {
	handler := New(Config{PublicURL: "https://groups.example", QQAppID: "test", QQAppKey: "test"}, nil, &memoryGroups{}).Routes()
	request := func(path, peer string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = peer
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", time.Now().UnixNano()%200))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for i := 0; i < 20; i++ {
		if w := request("/auth/qq", "192.0.2.1:1234"); w.Code != 302 {
			t.Fatalf("valid start %d: %d", i, w.Code)
		}
	}
	w := request("/auth/qq", "192.0.2.1:5678")
	if w.Code == 302 || w.Header().Get("Retry-After") == "" {
		t.Fatal("sequential login requests bypassed rate limit")
	}
	if w := request("/auth/qq", "192.0.2.2:1234"); w.Code != 302 {
		t.Fatal("one peer exhausted another peer's allowance")
	}
	if w := request("/auth/qq/callback", "192.0.2.1:1234"); w.Header().Get("Retry-After") != "" {
		t.Fatal("start rate limit blocked callback")
	}
}

func securityDatabase(t *testing.T) *pgxpool.Pool {
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
	schema := fmt.Sprintf("security_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err = db.Exec(ctx, migrations.SQL); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestPostgresCombinedCreationQuota(t *testing.T) {
	db := securityDatabase(t)
	ctx := context.Background()
	var owner int64
	if err := db.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('quota') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO groups(owner_id,organization,qq_number,join_url) SELECT $1,'quota',(700000000+n)::text,'https://qm.qq.com/q/test' FROM generate_series(1,50) n`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO sites(owner_id,name,domain,organization) SELECT $1,'quota','site'||n||'.example','quota' FROM generate_series(1,49) n`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,'csrf',now()+interval '1 hour')`, hashToken("quota-session"), owner); err != nil {
		t.Fatal(err)
	}
	handler := New(Config{PublicURL: "https://groups.example"}, db, groups.Store{DB: db}).Routes()
	request := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "nailist_session", Value: "quota-session"})
		r.Header.Set("X-CSRF-Token", "csrf")
		r.Header.Set("Origin", "https://groups.example")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	results := make(chan *httptest.ResponseRecorder, 2)
	go func() {
		results <- request("/api/groups", `{"organization":"quota","qqNumber":"800000001","joinUrl":"https://qm.qq.com/q/test"}`)
	}()
	go func() {
		results <- request("/api/sites", `{"name":"quota","organization":"quota","domain":"new.example"}`)
	}()
	statuses := map[int]int{}
	for i := 0; i < 2; i++ {
		w := <-results
		statuses[w.Code]++
		if w.Code != 201 && w.Code != 429 {
			t.Errorf("unexpected status: %d %s", w.Code, w.Body.String())
		}
	}
	if statuses[201] != 1 || statuses[429] != 1 {
		t.Fatalf("concurrent creates exceeded shared quota: %v", statuses)
	}
}

func TestPostgresDeletedHistoryCountsTowardsQuota(t *testing.T) {
	db := securityDatabase(t)
	ctx := context.Background()
	var owner int64
	if err := db.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('history') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO groups(owner_id,organization,qq_number,join_url,deleted_at) SELECT $1,'history',(710000000+n)::text,'https://qm.qq.com/q/test',now() FROM generate_series(1,1000) n`, owner); err != nil {
		t.Fatal(err)
	}
	_, err := (groups.Store{DB: db}).Save(ctx, owner, 0, groups.Group{Organization: "history", QQNumber: "800000001", JoinURL: "https://qm.qq.com/q/test", Keywords: []string{}})
	if !errors.Is(err, resources.ErrTotalQuota) {
		t.Fatalf("expected total storage quota, got %v", err)
	}
	_, err = (sites.Store{DB: db}).Save(ctx, owner, 0, sites.Site{Name: "history", Domain: "history.example", Organization: "history"})
	if !errors.Is(err, resources.ErrTotalQuota) {
		t.Fatalf("sites bypassed combined history quota: %v", err)
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM groups`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1000 {
		t.Fatalf("quota failure wrote a row: %d", count)
	}
}

func TestPostgresQuotaAllowsEditingDeletionAndOtherOwners(t *testing.T) {
	db := securityDatabase(t)
	ctx := context.Background()
	var owner, other int64
	if err := db.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('owner') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `INSERT INTO users(nickname) VALUES('other') RETURNING id`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO sites(owner_id,name,domain,organization) SELECT $1,'site','site'||n||'.example','school' FROM generate_series(1,100) n`, owner); err != nil {
		t.Fatal(err)
	}
	store := sites.Store{DB: db}
	site := sites.Site{Name: "updated", Domain: "updated.example", Organization: "school"}
	if _, err := store.Save(ctx, owner, 0, site); !errors.Is(err, resources.ErrActiveQuota) {
		t.Fatalf("active quota missing: %v", err)
	}
	if _, err := store.Save(ctx, other, 0, site); err != nil {
		t.Fatalf("quota affected another owner: %v", err)
	}
	var id int64
	if err := db.QueryRow(ctx, `SELECT id FROM sites WHERE owner_id=$1 ORDER BY id LIMIT 1`, owner).Scan(&id); err != nil {
		t.Fatal(err)
	}
	site.Domain = "edited.example"
	if _, err := store.Save(ctx, owner, id, site); err != nil {
		t.Fatalf("quota blocked editing: %v", err)
	}
	if err := store.Delete(ctx, owner, id); err != nil {
		t.Fatalf("quota blocked deletion: %v", err)
	}
	if _, err := store.Save(ctx, owner, 0, site); err != nil {
		t.Fatalf("deletion did not release active quota: %v", err)
	}
	var total, active int
	if err := db.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE deleted_at IS NULL) FROM sites WHERE owner_id=$1`, owner).Scan(&total, &active); err != nil {
		t.Fatal(err)
	}
	if total != 101 || active != 100 {
		t.Fatalf("history not preserved: total %d active %d", total, active)
	}
}

func TestUserWritesRateLimited(t *testing.T) {
	store := &memoryGroups{}
	handler := New(Config{PublicURL: "https://groups.example"}, nil, store).Routes()
	for i := 0; i < 31; i++ {
		body, _ := json.Marshal(groups.Input{Organization: "rate", QQNumber: fmt.Sprint(700000000 + i), JoinURL: "https://qm.qq.com/q/test"})
		r := httptest.NewRequest("POST", "/api/groups", strings.NewReader(string(body)))
		r = r.WithContext(context.WithValue(r.Context(), userKey, User{ID: 1, CSRF: "csrf"}))
		r.Header.Set("X-CSRF-Token", "csrf")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if i < 30 && w.Code != 201 {
			t.Fatalf("valid write rejected: %d", w.Code)
		}
		if i == 30 && (w.Code != 429 || w.Header().Get("Retry-After") == "") {
			t.Fatalf("write rate not limited: %d", w.Code)
		}
	}
}

func TestPostgresLoginRateLimitPrecedesSessionLookup(t *testing.T) {
	db := securityDatabase(t)
	handler := New(Config{PublicURL: "https://groups.example", QQAppID: "test", QQAppKey: "test"}, db, groups.Store{DB: db}).Routes()
	request := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = "192.0.2.3:1234"
		r.AddCookie(&http.Cookie{Name: "nailist_session", Value: "invalid-session"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for i := 0; i < 20; i++ {
		if w := request("GET", "/auth/qq"); w.Code != 302 {
			t.Fatalf("start failed: %d %s", w.Code, w.Body.String())
		}
	}
	acquired := db.Stat().AcquireCount()
	if w := request("GET", "/auth/qq"); w.Code != 303 {
		t.Fatalf("login rate not applied: %d", w.Code)
	}
	if w := request("POST", "/api/auth/bind/qq"); w.Code != 429 {
		t.Fatalf("binding bypassed auth budget: %d", w.Code)
	}
	if got := db.Stat().AcquireCount(); got != acquired {
		t.Fatalf("rejected requests still reached database: %d -> %d", acquired, got)
	}
	var flows int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM auth_flows`).Scan(&flows); err != nil {
		t.Fatal(err)
	}
	if flows != 20 {
		t.Fatalf("unexpected number of authorization flows: %d", flows)
	}
}
