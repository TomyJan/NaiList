package admin

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"nailist/internal/accounts"
	"nailist/internal/groups"
	"nailist/internal/logging"
	"time"
)

var ErrNotFound = errors.New("账号或登录方式不存在")
var ErrLastIdentity = errors.New("至少保留一种登录方式")
var ErrOwnAccount = errors.New("不能删除当前管理员账号")
var ErrActiveSites = errors.New("请先转移该账号的有效站点")
var ErrActiveGroups = errors.New("请先转移该账号的有效奶群")

type Store struct{ DB *pgxpool.Pool }
type MutationStats struct{ RevokedSessions, DeletedFlows, DeletedIdentities, DeletedGroups, DeletedSites int64 }
type User struct {
	ID         int64               `json:"id"`
	Nickname   string              `json:"nickname"`
	AvatarURL  string              `json:"avatarUrl"`
	SiteCount  int64               `json:"siteCount"`
	GroupCount int64               `json:"groupCount"`
	Identities []accounts.Identity `json:"identities"`
}
type Group struct {
	groups.Group
	OwnerID int64 `json:"ownerId"`
}

func (s Store) Users(ctx context.Context) ([]User, error) {
	// One statement keeps users, counts and bindings on the same database snapshot.
	rows, err := s.DB.Query(ctx, `SELECT u.id,u.nickname,u.avatar_url,(SELECT count(*) FROM groups g WHERE g.owner_id=u.id AND g.deleted_at IS NULL),(SELECT count(*) FROM sites s WHERE s.owner_id=u.id AND s.deleted_at IS NULL),COALESCE(i.provider_id,''),COALESCE(i.nickname,''),COALESCE(i.avatar_url,'') FROM users u LEFT JOIN identities i ON i.user_id=u.id ORDER BY u.id,i.created_at,i.id`)
	if err != nil {
		return nil, fmt.Errorf("list admin users: %w", err)
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		var i accounts.Identity
		if err := rows.Scan(&u.ID, &u.Nickname, &u.AvatarURL, &u.GroupCount, &u.SiteCount, &i.ProviderID, &i.Nickname, &i.AvatarURL); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].ID != u.ID {
			u.Identities = []accounts.Identity{}
			out = append(out, u)
		}
		if i.ProviderID != "" {
			out[len(out)-1].Identities = append(out[len(out)-1].Identities, i)
		}
	}
	return out, rows.Err()
}
func (s Store) Groups(ctx context.Context) ([]Group, error) {
	rows, err := (groups.Store{DB: s.DB}).List(ctx, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Group, 0, len(rows))
	for _, g := range rows {
		out = append(out, Group{g, g.OwnerID})
	}
	return out, nil
}
func rollback(ctx context.Context, tx pgx.Tx) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	err := tx.Rollback(cleanup)
	if errors.Is(err, pgx.ErrTxClosed) {
		return
	}
	outcome, reason, message := "success", "transaction_not_committed", "管理变更未提交，已回滚事务"
	if err != nil {
		outcome, reason, message = "failed", "rollback_failed", "管理变更未提交，事务回滚失败，连接将由驱动回收"
	}
	logging.FromContext(cleanup).DebugContext(cleanup, message, "component", "admin", "event", "admin.transaction.stage", "stage", "rollback", "outcome", outcome, "reason", reason, "error_kind", logging.ErrorKind(err))
}
func lockUser(ctx context.Context, tx pgx.Tx, id int64) error {
	var got int64
	err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func stage(ctx context.Context, stage string, args ...any) {
	fields := append([]any{"component", "admin", "event", "admin.transaction.stage", "stage", stage}, args...)
	message := map[string]string{"lock_user": "锁定目标账号，串行执行绑定及删除约束检查", "lock_owner": "锁定目标所属账号，协调账号删除与资源转移", "identity_count": "已检查目标账号剩余登录方式", "delete_identity": "准备删除指定登录方式", "resources_revoked": "已在事务中撤销会话及待完成绑定流程", "cleanup": "已在事务中清理账号历史资源", "commit": "全部管理变更已写入事务，准备提交"}[stage]
	logging.FromContext(ctx).DebugContext(ctx, message, fields...)
}
func revoke(ctx context.Context, tx pgx.Tx, id int64) (MutationStats, error) {
	stats := MutationStats{}
	tag, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id)
	if err != nil {
		return stats, err
	}
	stats.RevokedSessions = tag.RowsAffected()
	tag, err = tx.Exec(ctx, `DELETE FROM auth_flows WHERE bind_user_id=$1`, id)
	if err != nil {
		return stats, err
	}
	stats.DeletedFlows = tag.RowsAffected()
	stage(ctx, "resources_revoked", "account_id", id, "revoked_sessions", stats.RevokedSessions, "deleted_flows", stats.DeletedFlows)
	return stats, err
}
func (s Store) Unbind(ctx context.Context, id int64, provider string) (MutationStats, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return MutationStats{}, err
	}
	defer rollback(ctx, tx)
	stage(ctx, "lock_user", "account_id", id)
	if err = lockUser(ctx, tx, id); err != nil {
		return MutationStats{}, err
	}
	var exists bool
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(bool_or(provider_id=$2),false) FROM identities WHERE user_id=$1`, id, provider).Scan(&count, &exists); err != nil {
		return MutationStats{}, err
	}
	stage(ctx, "identity_count", "account_id", id, "identity_count", count, "provider_found", exists)
	if !exists {
		return MutationStats{}, ErrNotFound
	}
	if count <= 1 {
		return MutationStats{}, ErrLastIdentity
	}
	stage(ctx, "delete_identity", "account_id", id, "provider", provider)
	if _, err = tx.Exec(ctx, `DELETE FROM identities WHERE user_id=$1 AND provider_id=$2`, id, provider); err != nil {
		return MutationStats{}, err
	}
	revoked, err := revoke(ctx, tx, id)
	if err != nil {
		return MutationStats{}, err
	}
	revoked.DeletedIdentities = 1
	stage(ctx, "commit", "account_id", id)
	if err = tx.Commit(ctx); err != nil {
		return MutationStats{}, err
	}
	return revoked, nil
}
func (s Store) DeleteUser(ctx context.Context, actor, id int64) (MutationStats, error) {
	if actor == id {
		return MutationStats{}, ErrOwnAccount
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return MutationStats{}, err
	}
	defer rollback(ctx, tx)
	stage(ctx, "lock_user", "account_id", id)
	if err = lockUser(ctx, tx, id); err != nil {
		return MutationStats{}, err
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE owner_id=$1 AND deleted_at IS NULL)`, id).Scan(&active); err != nil {
		return MutationStats{}, err
	}
	if active {
		return MutationStats{}, ErrActiveGroups
	}
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sites WHERE owner_id=$1 AND deleted_at IS NULL)`, id).Scan(&active); err != nil {
		return MutationStats{}, err
	}
	if active {
		return MutationStats{}, ErrActiveSites
	}
	revoked, err := revoke(ctx, tx, id)
	if err != nil {
		return MutationStats{}, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM sites WHERE owner_id=$1`, id)
	if err != nil {
		return MutationStats{}, err
	}
	revoked.DeletedSites = tag.RowsAffected()
	for index, q := range []string{`DELETE FROM groups WHERE owner_id=$1`, `DELETE FROM identities WHERE user_id=$1`, `DELETE FROM users WHERE id=$1`} {
		tag, deleteErr := tx.Exec(ctx, q, id)
		if deleteErr != nil {
			return MutationStats{}, deleteErr
		}
		if index == 0 {
			revoked.DeletedGroups = tag.RowsAffected()
		}
		if index == 1 {
			revoked.DeletedIdentities = tag.RowsAffected()
		}
	}
	stage(ctx, "cleanup", "account_id", id, "deleted_groups", revoked.DeletedGroups, "deleted_sites", revoked.DeletedSites, "deleted_identities", revoked.DeletedIdentities)
	stage(ctx, "commit", "account_id", id)
	if err = tx.Commit(ctx); err != nil {
		return MutationStats{}, err
	}
	return revoked, nil
}
func (s Store) SaveGroup(ctx context.Context, id, owner int64, g groups.Group) (Group, int64, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Group{}, 0, err
	}
	defer rollback(ctx, tx)
	// Lock the destination before updating the group, matching account deletion's lock order.
	stage(ctx, "lock_owner", "group_id", id, "new_owner_id", owner)
	if err = lockUser(ctx, tx, owner); err != nil {
		return Group{}, 0, err
	}
	var old int64
	if err = tx.QueryRow(ctx, `SELECT owner_id FROM groups WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&old); errors.Is(err, pgx.ErrNoRows) {
		return Group{}, 0, groups.ErrNotFound
	} else if err != nil {
		return Group{}, 0, err
	}
	err = tx.QueryRow(ctx, `UPDATE groups SET owner_id=$2,organization=$3,keywords=$4,qq_number=$5,join_url=$6,updated_at=now() WHERE id=$1 RETURNING updated_at`, id, owner, g.Organization, g.Keywords, g.QQNumber, g.JoinURL).Scan(&g.UpdatedAt)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return Group{}, 0, groups.ErrDuplicate
	}
	if err != nil {
		return Group{}, 0, err
	}
	g.ID, g.OwnerID = id, owner
	stage(ctx, "commit", "group_id", id, "old_owner_id", old, "new_owner_id", owner)
	if err = tx.Commit(ctx); err != nil {
		return Group{}, 0, err
	}
	return Group{g, owner}, old, nil
}
