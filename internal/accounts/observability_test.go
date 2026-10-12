package accounts

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"nailist/internal/logging"
)

func TestPostgresResolveObservabilityCommitAndConflict(t *testing.T) {
	db := testDB(t)
	var output bytes.Buffer
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	store := Store{DB: db}
	var allLogs strings.Builder
	identity := Identity{ProviderID: "qq", Issuer: "app", Subject: "private-subject", UnionID: "private-unionid", Nickname: "private-nickname"}
	id, err := store.Resolve(ctx, identity, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"event":"account.created"`) || !strings.Contains(output.String(), `"created_or_existing":"created"`) || !strings.Contains(output.String(), `"lock_count":3`) {
		t.Fatal(output.String())
	}
	allLogs.WriteString(output.String())
	output.Reset()
	if got, err := store.Resolve(ctx, identity, 0, true); err != nil || got != id {
		t.Fatal(got, err)
	}
	if !strings.Contains(output.String(), `"created_or_existing":"existing"`) || strings.Contains(output.String(), `"event":"account.created"`) {
		t.Fatal(output.String())
	}
	allLogs.WriteString(output.String())
	output.Reset()
	bound := Identity{ProviderID: "campus", Issuer: "https://example.test", Subject: "private-bound-subject", Nickname: "private-bound-nickname"}
	if got, err := store.Resolve(ctx, bound, id, false); err != nil || got != id {
		t.Fatal(got, err)
	}
	if !strings.Contains(output.String(), `"event":"account.identity.bound"`) || !strings.Contains(output.String(), `"stage":"commit"`) {
		t.Fatal(output.String())
	}
	allLogs.WriteString(output.String())
	output.Reset()
	identity.UnionID = "private-conflicting-unionid"
	if _, err := store.Resolve(ctx, identity, 0, true); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"stage":"rollback"`) || strings.Contains(output.String(), `"level":"INFO"`) || strings.Contains(output.String(), `"level":"ERROR"`) {
		t.Fatal(output.String())
	}
	allLogs.WriteString(output.String())
	for _, secret := range []string{"private-subject", "private-unionid", "private-conflicting-unionid", "private-nickname", "private-bound-subject", "private-bound-nickname"} {
		if strings.Contains(allLogs.String(), secret) {
			t.Fatal(allLogs.String())
		}
	}
}

func TestPostgresResolveWrapsDependencyStep(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `ALTER TABLE identities RENAME TO identities_unavailable`); err != nil {
		t.Fatal(err)
	}
	_, err := (Store{DB: db}).Resolve(ctx, Identity{ProviderID: "campus", Issuer: "https://example.test", Subject: "private-subject"}, 0, false)
	if err == nil || !strings.Contains(err.Error(), "lookup identities") {
		t.Fatalf("missing dependency step: %v", err)
	}
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "42P01" {
		t.Fatalf("database error chain lost: %v", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = (Store{DB: db}).Resolve(ctx, Identity{ProviderID: "campus", Issuer: "https://example.test", Subject: "private-subject"}, 0, false)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "begin account resolution transaction") {
		t.Fatalf("cancellation chain or step lost: %v", err)
	}
}
