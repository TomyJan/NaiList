package logging

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestDatabaseTraceNamesStatementWithoutSQLOrParameters(t *testing.T) {
	var out bytes.Buffer
	logger, _ := New(&out, "debug")
	ctx := WithOperation(WithLogger(t.Context(), logger.With("request_id", "db-request")), "groups.insert")
	tracer := DatabaseTracer{}
	ctx = tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "INSERT INTO groups(join_url) VALUES($1) /* never-log-sql-secret */", Args: []any{"parameter-secret"}})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("INSERT 0 1")})
	text := out.String()
	for _, want := range []string{"event=db.query.started", "event=db.query.completed", "db_operation=groups.insert", "statement_id=", "statement_kind=INSERT", "table=groups", "rows_affected=1", "duration_ms=", "request_id=db-request"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s: %s", want, text)
		}
	}
	for _, secret := range []string{"parameter-secret", "never-log-sql-secret", "VALUES"} {
		if strings.Contains(text, secret) {
			t.Fatalf("leaked SQL data: %s", text)
		}
	}
}

func TestUpstreamTraceRetainsEndpointStatusAndCorrelation(t *testing.T) {
	var out bytes.Buffer
	logger, _ := New(&out, "debug")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "upstream-body-secret", 503) }))
	defer upstream.Close()
	client := TraceClient(upstream.Client())
	r, _ := http.NewRequestWithContext(WithLogger(t.Context(), logger.With("request_id", "upstream-request")), "GET", upstream.URL+"/token?code=query-secret", nil)
	r.Header.Set("Authorization", "Bearer header-secret")
	res, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	text := out.String()
	for _, want := range []string{"event=upstream.request.started", "event=upstream.request.completed", "upstream_path=/token", "upstream_status=503", "request_id=upstream-request", "duration_ms="} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s: %s", want, text)
		}
	}
	for _, secret := range []string{"query-secret", "header-secret", "upstream-body-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("leaked upstream secret: %s", text)
		}
	}
}

func TestErrorClassification(t *testing.T) {
	for _, test := range []struct {
		err  error
		kind string
	}{{context.Canceled, "canceled"}, {context.DeadlineExceeded, "timeout"}, {&pgconn.PgError{Code: "08006"}, "database"}, {errors.New("failure"), "internal"}} {
		if got := ErrorKind(test.err); got != test.kind {
			t.Fatalf("kind=%s want=%s", got, test.kind)
		}
	}
}
