package admin

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"nailist/internal/groups"
	"nailist/migrations"
	"os"
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
	root, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("admin_test_%d", time.Now().UnixNano())
	if _, err = root.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
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
	t.Cleanup(func() { db.Close(); _, _ = root.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); root.Close() })
	if _, err = db.Exec(ctx, migrations.SQL); err != nil {
		t.Fatal(err)
	}
	return db
}
func user(t *testing.T, db *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(context.Background(), `INSERT INTO users(nickname) VALUES('test') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPostgresTransferAndDeleteDestinationSerialize(t *testing.T) {
	db := testDB(t)
	store := Store{DB: db}
	ctx := context.Background()
	source := user(t, db)
	for i := 0; i < 12; i++ {
		target := user(t, db)
		var gid int64
		if err := db.QueryRow(ctx, `INSERT INTO groups(owner_id,organization,qq_number,join_url) VALUES($1,'old',$2,'https://qm.qq.com/q/old') RETURNING id`, source, fmt.Sprintf("12345%02d", i)).Scan(&gid); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		deleted := make(chan error, 1)
		transferred := make(chan error, 1)
		go func() { <-start; _, err := store.DeleteUser(ctx, source, target); deleted <- err }()
		go func() {
			<-start
			_, _, err := store.SaveGroup(ctx, gid, target, groups.Group{Organization: "new", QQNumber: fmt.Sprintf("12345%02d", i), JoinURL: "https://qm.qq.com/q/new", Keywords: []string{}})
			transferred <- err
		}()
		close(start)
		deleteErr, transferErr := <-deleted, <-transferred
		if deleteErr == nil {
			if !errors.Is(transferErr, ErrNotFound) {
				t.Fatalf("deleted destination accepted: %v", transferErr)
			}
		} else {
			if !errors.Is(deleteErr, ErrActiveGroups) || transferErr != nil {
				t.Fatalf("transfer/delete: %v / %v", transferErr, deleteErr)
			}
		}
		var orphan bool
		if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM groups g LEFT JOIN users u ON u.id=g.owner_id WHERE u.id IS NULL)`).Scan(&orphan); err != nil || orphan {
			t.Fatalf("orphan owner: %v %v", orphan, err)
		}
	}
}

func TestPostgresUnbindAndDeletionReportCommittedCleanupCounts(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := Store{DB: db}
	id := user(t, db)
	for _, provider := range []string{"qq", "campus"} {
		if _, err := db.Exec(ctx, `INSERT INTO identities(user_id,provider_id,issuer,subject,nickname) VALUES($1,$2,'issuer',$2,'test')`, id, provider); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `INSERT INTO sessions VALUES('one',$1,'csrf',now()+interval '1 hour'),('two',$1,'csrf',now()+interval '1 hour')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO auth_flows VALUES('pending','qq',$1,'one','nonce','verifier',now()+interval '1 hour')`, id); err != nil {
		t.Fatal(err)
	}
	stats, err := store.Unbind(ctx, id, "qq")
	if err != nil || stats.RevokedSessions != 2 || stats.DeletedFlows != 1 || stats.DeletedIdentities != 1 {
		t.Fatalf("unbind stats: %+v %v", stats, err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO groups(owner_id,organization,qq_number,join_url,deleted_at) VALUES($1,'old','12345','https://qm.qq.com/q/old',now())`, id); err != nil {
		t.Fatal(err)
	}
	stats, err = store.DeleteUser(ctx, id+1, id)
	if err != nil || stats.DeletedGroups != 1 || stats.DeletedIdentities != 1 {
		t.Fatalf("delete stats: %+v %v", stats, err)
	}
}
