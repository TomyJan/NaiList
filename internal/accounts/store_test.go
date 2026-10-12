package accounts

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"nailist/migrations"
	"os"
	"sync"
	"testing"
	"time"
)

func testDB(t *testing.T) *pgxpool.Pool {
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
	schema := fmt.Sprintf("accounts_test_%d", time.Now().UnixNano())
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

func TestPostgresIdentityLoginAndPermanentBinding(t *testing.T) {
	db := testDB(t)
	store := Store{DB: db}
	ctx := context.Background()
	qq := Identity{ProviderID: "qq", Issuer: "app", Subject: "openid", Nickname: "QQ 用户"}
	id, err := store.Resolve(ctx, qq, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	oidc := Identity{ProviderID: "campus", Issuer: "https://id.example", Subject: "user1", Nickname: "学校用户"}
	bound, err := store.Resolve(ctx, oidc, id, false)
	if err != nil || bound != id {
		t.Fatalf("binding: %d %v", bound, err)
	}
	loggedIn, err := store.Resolve(ctx, oidc, 0, false)
	if err != nil || loggedIn != id {
		t.Fatalf("login: %d %v", loggedIn, err)
	}
	oidc.Subject = "user2"
	if _, err = store.Resolve(ctx, oidc, id, false); !errors.Is(err, ErrAlreadyBound) {
		t.Fatalf("second identity: %v", err)
	}
	other, err := store.Resolve(ctx, oidc, 0, false)
	if err != nil || other == id {
		t.Fatal("independent login must create separate account", err)
	}
	oidc.Subject = "user1"
	if _, err = store.Resolve(ctx, oidc, other, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("identity theft: %v", err)
	}
	identities, err := store.List(ctx, id)
	if err != nil || len(identities) != 2 {
		t.Fatalf("bindings: %v %v", identities, err)
	}
}

func TestPostgresQQUnionIDKeepsExistingAccount(t *testing.T) {
	db := testDB(t)
	store := Store{DB: db}
	ctx := context.Background()
	qq := Identity{ProviderID: "qq", Issuer: "app", Subject: "openid", Nickname: "用户"}
	id, err := store.Resolve(ctx, qq, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	qq.UnionID = "union"
	got, err := store.Resolve(ctx, qq, 0, true)
	if err != nil || got != id {
		t.Fatal("union upgrade lost account", err)
	}
	qq.UnionID = "different-union"
	if _, err := store.Resolve(ctx, qq, 0, true); !errors.Is(err, ErrConflict) {
		t.Fatal("UnionID replaced", err)
	}
	qq.UnionID = "union"
	qq.Issuer = "second-app"
	qq.Subject = "other-openid"
	got, err = store.Resolve(ctx, qq, 0, true)
	if err != nil || got != id {
		t.Fatal("union login lost account", err)
	}
	qq.UnionID = ""
	if _, err = store.Resolve(ctx, qq, 0, true); err == nil {
		t.Fatal("required unionid missing")
	}
}

func TestPostgresConcurrentIdentityLogin(t *testing.T) {
	db := testDB(t)
	store := Store{DB: db}
	ctx := context.Background()
	identity := Identity{ProviderID: "campus", Issuer: "https://id.example", Subject: "same", Nickname: "用户"}
	var wg sync.WaitGroup
	ids := make(chan int64, 6)
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); id, err := store.Resolve(ctx, identity, 0, false); ids <- id; errs <- err }()
	}
	wg.Wait()
	close(ids)
	close(errs)
	var first int64
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for id := range ids {
		if first == 0 {
			first = id
		}
		if id != first {
			t.Fatal("duplicate accounts")
		}
	}
}

func TestPostgresLegacyQQMigrationPreservesOwnership(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `DROP TABLE identities; ALTER TABLE users ADD COLUMN qq_openid TEXT UNIQUE; INSERT INTO users(qq_openid,nickname) VALUES('legacy-openid','老用户'); INSERT INTO groups(owner_id,organization,qq_number,join_url) SELECT id,'原学校','123456789','https://qm.qq.com/q/test' FROM users WHERE qq_openid='legacy-openid'; INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) SELECT 'session',id,'csrf',now()+interval '1 day' FROM users WHERE qq_openid='legacy-openid'`); err != nil {
		t.Fatal(err)
	}
	var owner int64
	if err := db.QueryRow(ctx, `SELECT id FROM users WHERE qq_openid='legacy-openid'`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := db.Exec(ctx, migrations.SQL); err != nil {
			t.Fatal("migration not idempotent", err)
		}
	}
	id, err := (Store{DB: db}).Resolve(ctx, Identity{ProviderID: "qq", Issuer: "original-app", Subject: "legacy-openid", Nickname: "老用户"}, 0, false)
	if err != nil || id != owner {
		t.Fatal("legacy account lost", err)
	}
	var groupOwner, sessionOwner int64
	if err := db.QueryRow(ctx, `SELECT owner_id FROM groups WHERE qq_number='123456789'`).Scan(&groupOwner); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT user_id FROM sessions WHERE token_hash='session'`).Scan(&sessionOwner); err != nil {
		t.Fatal(err)
	}
	if groupOwner != owner || sessionOwner != owner {
		t.Fatal("ownership changed")
	}
	var issuer string
	if err := db.QueryRow(ctx, `SELECT issuer FROM identities WHERE user_id=$1`, owner).Scan(&issuer); err != nil || issuer != "original-app" {
		t.Fatal("legacy issuer not claimed", err)
	}
}
