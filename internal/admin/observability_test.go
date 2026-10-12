package admin

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"nailist/internal/logging"
	"strings"
	"testing"
)

func TestPostgresAdminRollbackLogsOnlyUncommittedWork(t *testing.T) {
	db := testDB(t)
	store := Store{DB: db}
	id := user(t, db)
	var output bytes.Buffer
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if _, err := db.Exec(ctx, `INSERT INTO identities(user_id,provider_id,issuer,subject,nickname) VALUES($1,'qq','private-issuer','private-subject','private-nickname')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Unbind(ctx, id, "qq"); !errors.Is(err, ErrLastIdentity) {
		t.Fatal(err)
	}
	logs := output.String()
	for _, field := range []string{`"stage":"rollback"`, `"outcome":"success"`, `"reason":"transaction_not_committed"`, `"error_kind":"none"`} {
		if !strings.Contains(logs, field) {
			t.Fatalf("missing rollback field %s: %s", field, logs)
		}
	}
	for _, secret := range []string{"private-issuer", "private-subject", "private-nickname"} {
		if strings.Contains(logs, secret) {
			t.Fatal(logs)
		}
	}
	if _, err := db.Exec(ctx, `INSERT INTO identities(user_id,provider_id,issuer,subject,nickname) VALUES($1,'campus','issuer','subject','test')`, id); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if _, err := store.Unbind(ctx, id, "qq"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), `"stage":"rollback"`) {
		t.Fatalf("committed transaction logged rollback: %s", &output)
	}
}

func TestPostgresFailedFlowRevocationDoesNotReportResourcesRevoked(t *testing.T) {
	db := testDB(t)
	id := user(t, db)
	var output bytes.Buffer
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if _, err := db.Exec(ctx, `ALTER TABLE auth_flows RENAME TO unavailable_auth_flows`); err != nil {
		t.Fatal(err)
	}
	if _, err := (Store{DB: db}).DeleteUser(ctx, id+1, id); err == nil {
		t.Fatal("missing flow table unexpectedly accepted")
	}
	if strings.Contains(output.String(), `"stage":"resources_revoked"`) {
		t.Fatalf("failed revocation reported success: %s", &output)
	}
	if !strings.Contains(output.String(), `"stage":"rollback"`) {
		t.Fatal(output.String())
	}
}
