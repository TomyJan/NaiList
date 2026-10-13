// Package resources enforces the shared storage allowance of groups and sites.
package resources

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"nailist/internal/logging"
)

const ActiveLimit = 100
const TotalLimit = 1000

var ErrActiveQuota = errors.New("奶群和奶站合计最多保留 100 条，请先删除不再使用的记录")
var ErrTotalQuota = errors.New("账号累计上传已达 1000 条（含已删除记录），请联系站点管理员")

// Create serializes both resource types across all instances sharing this DB.
// Existing records may still be edited or deleted when the quota is exhausted.
func Create(ctx context.Context, db *pgxpool.Pool, owner int64, insert func(pgx.Tx) error) error {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("开始上传配额事务: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logging.FromContext(ctx).WarnContext(cleanup, "上传配额事务回滚未完成，连接由驱动回收", append([]any{"component", "database", "event", "resources.quota.rollback_failed"}, logging.ErrorAttrs(err)...)...)
		}
	}()
	var lockedID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, owner).Scan(&lockedID); err != nil {
		return fmt.Errorf("锁定上传账号: %w", err)
	}
	var active, total int
	if err = tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE deleted_at IS NULL), count(*) FROM (
		SELECT deleted_at FROM groups WHERE owner_id=$1
		UNION ALL SELECT deleted_at FROM sites WHERE owner_id=$1
	) records`, owner).Scan(&active, &total); err != nil {
		return fmt.Errorf("读取账号共享上传配额: %w", err)
	}
	logging.FromContext(ctx).DebugContext(ctx, "已在账号排他锁内核对奶群与奶站共享配额", "component", "resources", "event", "resources.quota.checked", "owner_id", owner, "active_count", active, "active_limit", ActiveLimit, "total_count", total, "total_limit", TotalLimit)
	if total >= TotalLimit {
		return ErrTotalQuota
	}
	if active >= ActiveLimit {
		return ErrActiveQuota
	}
	if err = insert(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交上传与配额事务: %w", err)
	}
	return nil
}
