package logging

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DatabaseTracer intentionally never logs raw SQL, argument values or row data.
type DatabaseTracer struct{}
type queryTraceKey struct{}
type connectTraceKey struct{}
type acquireTraceKey struct{}
type dbTrace struct {
	started time.Time
	logger  *slog.Logger
}

var tablePattern = regexp.MustCompile(`(?i)\b(?:FROM|INTO|UPDATE|JOIN)\s+(groups|users|sessions|identities|auth_flows)\b`)

func (DatabaseTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	kind := "OTHER"
	if fields := strings.Fields(data.SQL); len(fields) > 0 {
		switch first := strings.ToUpper(fields[0]); first {
		case "SELECT", "INSERT", "UPDATE", "DELETE", "BEGIN", "COMMIT", "ROLLBACK", "CREATE", "ALTER", "SET":
			kind = first
		}
	}
	table := "none"
	if match := tablePattern.FindStringSubmatch(data.SQL); len(match) > 1 {
		table = strings.ToLower(match[1])
	}
	operation, _ := ctx.Value(operationKey).(string)
	if operation == "" {
		operation = "unlabeled"
	}
	logger := FromContext(ctx).With("component", "database", "db_operation", operation, "statement_id", fmt.Sprintf("%x", sha256.Sum256([]byte(data.SQL)))[:12], "statement_kind", kind, "table", table)
	logger.DebugContext(ctx, "执行数据库语句", "event", "db.query.started", "argument_count", len(data.Args))
	return context.WithValue(ctx, queryTraceKey{}, dbTrace{time.Now(), logger})
}
func (DatabaseTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	trace, ok := ctx.Value(queryTraceKey{}).(dbTrace)
	if !ok {
		return
	}
	args := []any{"event", "db.query.completed", "duration_ms", DurationMS(trace.started), "rows_affected", data.CommandTag.RowsAffected(), "outcome", "success"}
	msg := "数据库语句执行完成"
	if data.Err != nil {
		msg = "数据库语句执行失败，交由业务层处理"
		args[len(args)-1] = "failed"
		args = append(args, ErrorAttrs(data.Err)...)
	}
	trace.logger.DebugContext(ctx, msg, args...)
	if time.Since(trace.started) >= time.Second && ErrorKind(data.Err) != "canceled" {
		trace.logger.WarnContext(ctx, "数据库语句耗时超过 1 秒，检查连接负载、锁等待和索引", "event", "db.query.slow", "duration_ms", DurationMS(trace.started), "threshold_ms", 1000)
	}
}
func (DatabaseTracer) TraceConnectStart(ctx context.Context, data pgx.TraceConnectStartData) context.Context {
	logger := FromContext(ctx).With("component", "database", "db_host", data.ConnConfig.Host, "db_port", data.ConnConfig.Port, "database", data.ConnConfig.Database)
	logger.DebugContext(ctx, "建立 PostgreSQL 物理连接", "event", "db.connection.started", "connect_timeout_ms", data.ConnConfig.ConnectTimeout.Milliseconds())
	return context.WithValue(ctx, connectTraceKey{}, dbTrace{time.Now(), logger})
}
func (DatabaseTracer) TraceConnectEnd(ctx context.Context, data pgx.TraceConnectEndData) {
	trace, ok := ctx.Value(connectTraceKey{}).(dbTrace)
	if !ok {
		return
	}
	args := []any{"event", "db.connection.completed", "duration_ms", DurationMS(trace.started), "outcome", "success"}
	if data.Err != nil {
		args[len(args)-1] = "failed"
		args = append(args, ErrorAttrs(data.Err)...)
	}
	level := slog.LevelDebug
	if data.Err != nil && ErrorKind(data.Err) != "canceled" {
		level = slog.LevelWarn
	}
	trace.logger.Log(ctx, level, "PostgreSQL 连接建立结束", args...)
}
func (DatabaseTracer) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	return context.WithValue(ctx, acquireTraceKey{}, time.Now())
}
func (DatabaseTracer) TraceAcquireEnd(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	started, ok := ctx.Value(acquireTraceKey{}).(time.Time)
	if !ok {
		return
	}
	stat := pool.Stat()
	args := []any{"component", "database", "event", "db.pool.acquired", "wait_ms", DurationMS(started), "pool_total", stat.TotalConns(), "pool_idle", stat.IdleConns(), "pool_acquired", stat.AcquiredConns(), "pool_max", stat.MaxConns(), "outcome", "success"}
	if data.Err != nil {
		args[len(args)-1] = "failed"
		args = append(args, ErrorAttrs(data.Err)...)
	}
	FromContext(ctx).DebugContext(ctx, "数据库连接池获取连接结束", args...)
	if time.Since(started) >= time.Second && ErrorKind(data.Err) != "canceled" {
		FromContext(ctx).WarnContext(ctx, "等待数据库连接超过 1 秒，检查连接池容量及长事务", "component", "database", "event", "db.pool.slow", "wait_ms", DurationMS(started), "pool_acquired", stat.AcquiredConns(), "pool_max", stat.MaxConns())
	}
}
