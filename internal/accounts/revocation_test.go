package accounts

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPostgresResolveSessionRejectsIdentityRevokedWhileWaitingForUserLock(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := Store{DB: db}
	identity := Identity{ProviderID: "qq", Issuer: "app", Subject: "revoked", Nickname: "test"}
	id, err := store.Resolve(ctx, identity, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := store.ResolveAuthenticated(ctx, identity, 0, false, Session{TokenHash: "new-session", CSRFToken: "csrf"})
		result <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=current_setting('application_name') AND query='SELECT id FROM users WHERE id=$1 FOR UPDATE' AND wait_event_type='Lock' AND pid<>pg_backend_pid())`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("resolution did not acquire user lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM identities WHERE user_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, ErrIdentityRevoked) {
		t.Fatalf("revoked identity issued session: %v", err)
	}
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("old account sessions: %d %v", count, err)
	}
}

func TestPostgresResolveAuthenticatedChecksBindingSessionInsideTransaction(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := Store{DB: db}
	identity := Identity{ProviderID: "qq", Issuer: "app", Subject: "owner", Nickname: "test"}
	id, err := store.Resolve(ctx, identity, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	bind := Identity{ProviderID: "campus", Issuer: "issuer", Subject: "new", Nickname: "test"}
	if _, err = store.ResolveAuthenticated(ctx, bind, id, false, Session{BindingSessionHash: "revoked"}); !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("revoked binding session accepted: %v", err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO sessions VALUES('live',$1,'csrf',now()+interval '1 hour')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ResolveAuthenticated(ctx, bind, id, false, Session{BindingSessionHash: "live"}); err != nil {
		t.Fatal(err)
	}
	loginID, err := store.ResolveAuthenticated(ctx, bind, 0, false, Session{TokenHash: "login", CSRFToken: "csrf"})
	if err != nil || loginID != id {
		t.Fatalf("login: %d %v", loginID, err)
	}
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE token_hash='login' AND user_id=$1`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("atomic session missing: %d %v", count, err)
	}
}

func TestPostgresBindingSessionExpiryWhileWaitingForAccountLock(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := Store{DB: db}
	id, err := store.Resolve(ctx, Identity{ProviderID: "qq", Issuer: "app", Subject: "owner", Nickname: "test"}, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO sessions VALUES('expiring',$1,'csrf',clock_timestamp()+interval '200 milliseconds')`, id); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := store.ResolveAuthenticated(ctx, Identity{ProviderID: "campus", Issuer: "issuer", Subject: "new", Nickname: "test"}, id, false, Session{BindingSessionHash: "expiring"})
		result <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=current_setting('application_name') AND query='SELECT id FROM users WHERE id=$1 FOR UPDATE' AND wait_event_type='Lock' AND pid<>pg_backend_pid())`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("binding did not reach account lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = db.Exec(ctx, `UPDATE sessions SET expires_at=clock_timestamp()-interval '1 millisecond' WHERE token_hash='expiring'`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, ErrSessionRevoked) {
		t.Fatalf("binding accepted a session expired while blocked: %v", err)
	}
}
