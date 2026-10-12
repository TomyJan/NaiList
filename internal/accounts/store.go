package accounts

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"sort"
	"time"

	"nailist/internal/logging"
)

var ErrConflict = errors.New("此登录身份已绑定其他账号")
var ErrAlreadyBound = errors.New("此账号已绑定该登录方式")
var ErrIdentityRevoked = errors.New("登录身份已撤销，请重新登录")
var ErrSessionRevoked = errors.New("绑定会话已撤销，请重新登录")

// Session is persisted in the same transaction as account resolution. Binding
// uses its initiating session instead of issuing a new one.
type Session struct{ TokenHash, CSRFToken, BindingSessionHash string }

type Identity struct {
	ProviderID string `json:"providerId"`
	Issuer     string `json:"-"`
	Subject    string `json:"-"`
	UnionID    string `json:"-"`
	Nickname   string `json:"nickname"`
	AvatarURL  string `json:"avatarUrl"`
}
type Store struct{ DB *pgxpool.Pool }

func (s Store) List(ctx context.Context, userID int64) ([]Identity, error) {
	rows, err := s.DB.Query(ctx, `SELECT provider_id,nickname,avatar_url FROM identities WHERE user_id=$1 ORDER BY created_at,id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list account identities: %w", err)
	}
	defer rows.Close()
	result := []Identity{}
	for rows.Next() {
		var identity Identity
		if err := rows.Scan(&identity.ProviderID, &identity.Nickname, &identity.AvatarURL); err != nil {
			return nil, fmt.Errorf("scan account identity: %w", err)
		}
		result = append(result, identity)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account identities: %w", err)
	}
	return result, nil
}

// Resolve keeps accounts independent from providers. Binding never merges accounts.
func (s Store) Resolve(ctx context.Context, identity Identity, bindUserID int64, useUnionID bool) (resolvedID int64, resolveErr error) {
	return s.resolve(ctx, identity, bindUserID, useUnionID, nil)
}

func (s Store) ResolveAuthenticated(ctx context.Context, identity Identity, bindUserID int64, useUnionID bool, session Session) (int64, error) {
	return s.resolve(ctx, identity, bindUserID, useUnionID, &session)
}

func (s Store) resolve(ctx context.Context, identity Identity, bindUserID int64, useUnionID bool, session *Session) (resolvedID int64, resolveErr error) {
	started := time.Now()
	ctx = logging.WithOperation(ctx, "account.resolve")
	intent := "login"
	if bindUserID != 0 {
		intent = "bind"
	}
	logger := logging.FromContext(ctx).With("component", "accounts", "provider", identity.ProviderID, "intent", intent)
	debug := func(stage, outcome, reason, msg string, fields ...any) {
		attrs := append([]any{"event", "account.resolve.stage", "stage", stage, "outcome", outcome, "reason", reason}, fields...)
		logger.DebugContext(ctx, msg, attrs...)
	}
	createdOrExisting := "existing"
	defer func() {
		outcome, reason := "success", "resolved"
		if resolveErr != nil {
			outcome, reason = "failed", "dependency_failed"
			if errors.Is(resolveErr, ErrConflict) {
				outcome, reason = "rejected", "identity_conflict"
			}
			if errors.Is(resolveErr, ErrAlreadyBound) {
				outcome, reason = "rejected", "already_bound"
			}
			if errors.Is(resolveErr, ErrIdentityRevoked) || errors.Is(resolveErr, ErrSessionRevoked) {
				outcome, reason = "rejected", "authorization_revoked"
			}
		}
		logger.DebugContext(ctx, "账号解析结束", "event", "account.resolve.completed", "stage", "resolve", "outcome", outcome, "reason", reason, "account_id", resolvedID, "created_or_existing", createdOrExisting, "duration_ms", logging.DurationMS(started))
	}()
	debug("validation", "started", "", "开始校验登录身份", "subject_present", identity.Subject != "", "union_id_present", identity.UnionID != "", "union_id_required", identity.ProviderID == "qq" && useUnionID)
	if identity.ProviderID == "" || identity.Issuer == "" || identity.Subject == "" {
		return 0, errors.New("登录身份不完整")
	}
	if identity.ProviderID == "qq" && useUnionID && identity.UnionID == "" {
		return 0, errors.New("QQ 未返回 UnionID")
	}
	if identity.ProviderID != "qq" || !useUnionID {
		identity.UnionID = ""
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin account resolution transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		err := tx.Rollback(cleanup)
		outcome := "success"
		if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			outcome = "failed"
		}
		debug("rollback", outcome, "resolution_not_committed", "账号解析未提交，回滚事务", "duration_ms", logging.DurationMS(started), "error_kind", logging.ErrorKind(err))
	}()
	debug("transaction", "success", "transaction_started", "已开始账号解析事务")
	locks := []string{identity.ProviderID + ":" + identity.Issuer + ":" + identity.Subject}
	if identity.ProviderID == "qq" {
		locks = append(locks, "qq:legacy:"+identity.Subject)
	}
	if identity.UnionID != "" {
		locks = append(locks, "qq:union:"+identity.UnionID)
	}
	sort.Strings(locks)
	debug("lock", "started", "", "开始获取身份事务锁", "lock_count", len(locks), "binding_user_lock", bindUserID != 0)
	for _, key := range locks {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
			return 0, fmt.Errorf("lock account identity: %w", err)
		}
	}
	if bindUserID != 0 {
		var id int64
		if err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, bindUserID).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, ErrSessionRevoked
			}
			return 0, fmt.Errorf("lock binding account: %w", err)
		}
		if session != nil {
			var hash string
			err = tx.QueryRow(ctx, `SELECT token_hash FROM sessions WHERE token_hash=$1 AND user_id=$2 AND expires_at>statement_timestamp() FOR SHARE`, session.BindingSessionHash, bindUserID).Scan(&hash)
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, ErrSessionRevoked
			}
			if err != nil {
				return 0, fmt.Errorf("check binding session in transaction: %w", err)
			}
		}
	}
	debug("lock", "success", "locks_acquired", "已获取账号解析所需事务锁", "lock_count", len(locks))
	rows, err := tx.Query(ctx, `SELECT id,user_id,COALESCE(qq_unionid,''),issuer=$2 AND subject=$3,issuer='' AND subject=$3,COALESCE($4<>'' AND qq_unionid=$4,false) FROM identities WHERE provider_id=$1 AND ((issuer=$2 AND subject=$3) OR ($1='qq' AND issuer='' AND subject=$3) OR ($1='qq' AND $4<>'' AND qq_unionid=$4))`, identity.ProviderID, identity.Issuer, identity.Subject, identity.UnionID)
	if err != nil {
		return 0, fmt.Errorf("lookup identities: %w", err)
	}
	var identityID, userID int64
	for rows.Next() {
		var rowID, rowUser int64
		var storedUnionID string
		var matched, legacy, union bool
		if err = rows.Scan(&rowID, &rowUser, &storedUnionID, &matched, &legacy, &union); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan matching identity: %w", err)
		}
		debug("identity_lookup", "success", "identity_matched", "找到可关联的登录身份", "matched", matched, "legacy", legacy, "union", union, "account_id", rowUser)
		if identity.UnionID != "" && storedUnionID != "" && identity.UnionID != storedUnionID {
			rows.Close()
			return 0, ErrConflict
		}
		if identityID != 0 && identityID != rowID {
			rows.Close()
			return 0, ErrConflict
		}
		identityID, userID = rowID, rowUser
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, fmt.Errorf("iterate matching identities: %w", err)
	}
	debug("identity_lookup", "success", "lookup_completed", "登录身份查询结束", "matched", identityID != 0)
	if bindUserID == 0 && userID != 0 {
		var lockedID int64
		if err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&lockedID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, ErrIdentityRevoked
			}
			return 0, fmt.Errorf("lock login account: %w", err)
		}
		// An administrator may have revoked the identity while this transaction
		// waited for the user lock. Never restore that identity or old session.
		var live bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identities WHERE id=$1 AND user_id=$2)`, identityID, userID).Scan(&live); err != nil {
			return 0, err
		}
		if !live {
			return 0, ErrIdentityRevoked
		}
		debug("lock", "success", "login_account_locked", "已锁定登录账号并确认身份仍有效", "account_id", userID)
	}
	if bindUserID != 0 && userID != 0 && userID != bindUserID {
		return 0, ErrConflict
	}
	if bindUserID != 0 {
		userID = bindUserID
		var boundID int64
		err = tx.QueryRow(ctx, `SELECT id FROM identities WHERE user_id=$1 AND provider_id=$2`, userID, identity.ProviderID).Scan(&boundID)
		if err == nil && boundID != identityID {
			return 0, ErrAlreadyBound
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("check existing provider binding: %w", err)
		}
		debug("binding", "success", "binding_available", "已确认账号可绑定此登录方式", "account_id", userID, "existing_binding", err == nil)
	}
	if userID == 0 {
		createdOrExisting = "created"
		debug("account", "started", "create_account", "首次登录，准备创建账号")
		err = tx.QueryRow(ctx, `INSERT INTO users(nickname,avatar_url) VALUES($1,$2) RETURNING id`, identity.Nickname, identity.AvatarURL).Scan(&userID)
		if err != nil {
			return 0, fmt.Errorf("create account: %w", err)
		}
	} else {
		debug("account", "success", "reuse_account", "复用已存在账号", "account_id", userID)
	}
	if identityID == 0 {
		debug("identity_write", "started", "create_identity", "准备保存新的登录身份", "account_id", userID)
		_, err = tx.Exec(ctx, `INSERT INTO identities(user_id,provider_id,issuer,subject,qq_unionid,nickname,avatar_url) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7)`, userID, identity.ProviderID, identity.Issuer, identity.Subject, identity.UnionID, identity.Nickname, identity.AvatarURL)
	} else {
		debug("identity_write", "started", "update_identity", "准备更新已关联身份的资料", "account_id", userID)
		_, err = tx.Exec(ctx, `UPDATE identities SET issuer=$2,subject=$3,qq_unionid=COALESCE(NULLIF($4,''),qq_unionid),nickname=$5,avatar_url=$6 WHERE id=$1`, identityID, identity.Issuer, identity.Subject, identity.UnionID, identity.Nickname, identity.AvatarURL)
	}
	var constraint *pgconn.PgError
	if errors.As(err, &constraint) && constraint.Code == "23505" {
		return 0, ErrConflict
	}
	if err != nil {
		return 0, fmt.Errorf("save account identity: %w", err)
	}
	debug("identity_write", "success", "identity_saved", "登录身份已写入事务", "account_id", userID)
	if bindUserID == 0 {
		if _, err = tx.Exec(ctx, `UPDATE users SET nickname=$2,avatar_url=$3 WHERE id=$1`, userID, identity.Nickname, identity.AvatarURL); err != nil {
			return 0, fmt.Errorf("refresh account profile: %w", err)
		}
	}
	debug("commit", "started", "", "准备提交账号解析事务", "account_id", userID)
	if session != nil && bindUserID == 0 {
		if session.TokenHash == "" || session.CSRFToken == "" {
			return 0, errors.New("登录会话不完整")
		}
		if _, err = tx.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,$3,now()+interval '7 days')`, session.TokenHash, userID, session.CSRFToken); err != nil {
			return 0, fmt.Errorf("create resolved account session: %w", err)
		}
		debug("session", "success", "session_saved", "已将登录会话写入账号事务", "account_id", userID)
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit account resolution: %w", err)
	}
	committed = true
	debug("commit", "success", "committed", "账号解析事务已提交", "account_id", userID, "created_or_existing", createdOrExisting, "duration_ms", logging.DurationMS(started))
	if createdOrExisting == "created" {
		logger.InfoContext(ctx, "首次登录已创建账号并关联身份", "event", "account.created", "stage", "commit", "outcome", "success", "reason", "new_identity", "account_id", userID, "created_or_existing", createdOrExisting)
	} else if bindUserID != 0 && identityID == 0 {
		logger.InfoContext(ctx, "已为账号绑定新的登录方式", "event", "account.identity.bound", "stage", "commit", "outcome", "success", "reason", "binding_created", "account_id", userID, "created_or_existing", createdOrExisting)
	}
	return userID, nil
}
