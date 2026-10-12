package logging

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type contextKey uint8

const (
	loggerKey contextKey = iota
	operationKey
)

func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

// WithOperation names a database action without exposing SQL or its arguments.
func WithOperation(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, operationKey, operation)
}

func DurationMS(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000
}

func ErrorKind(err error) string {
	var pg *pgconn.PgError
	var network net.Error
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.As(err, &pg):
		return "database"
	case errors.As(err, &network):
		if network.Timeout() {
			return "timeout"
		}
		return "network"
	case errors.Is(err, os.ErrPermission):
		return "permission"
	default:
		return "internal"
	}
}

// ErrorAttrs retains the cause/type and SQLSTATE, never PostgreSQL Detail (row values).
func ErrorAttrs(err error) []any {
	if err == nil {
		return nil
	}
	root := err
	for errors.Unwrap(root) != nil {
		root = errors.Unwrap(root)
	}
	attrs := []any{"error", err, "error_kind", ErrorKind(err), "error_type", reflect.TypeOf(root).String()}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		attrs = append(attrs, "sqlstate", pg.Code, "constraint", pg.ConstraintName)
	}
	return attrs
}
