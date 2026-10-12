package accounts

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"sort"
)

var ErrConflict = errors.New("此登录身份已绑定其他账号")
var ErrAlreadyBound = errors.New("此账号已绑定该登录方式")

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
		return nil, err
	}
	defer rows.Close()
	result := []Identity{}
	for rows.Next() {
		var identity Identity
		if err := rows.Scan(&identity.ProviderID, &identity.Nickname, &identity.AvatarURL); err != nil {
			return nil, err
		}
		result = append(result, identity)
	}
	return result, rows.Err()
}

// Resolve keeps accounts independent from providers. Binding never merges accounts.
func (s Store) Resolve(ctx context.Context, identity Identity, bindUserID int64, useUnionID bool) (int64, error) {
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
		return 0, err
	}
	defer tx.Rollback(ctx)
	locks := []string{identity.ProviderID + ":" + identity.Issuer + ":" + identity.Subject}
	if identity.ProviderID == "qq" {
		locks = append(locks, "qq:legacy:"+identity.Subject)
	}
	if identity.UnionID != "" {
		locks = append(locks, "qq:union:"+identity.UnionID)
	}
	sort.Strings(locks)
	for _, key := range locks {
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
			return 0, err
		}
	}
	if bindUserID != 0 {
		var id int64
		if err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, bindUserID).Scan(&id); err != nil {
			return 0, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT id,user_id,COALESCE(qq_unionid,'') FROM identities WHERE provider_id=$1 AND ((issuer=$2 AND subject=$3) OR ($1='qq' AND issuer='' AND subject=$3) OR ($1='qq' AND $4<>'' AND qq_unionid=$4))`, identity.ProviderID, identity.Issuer, identity.Subject, identity.UnionID)
	if err != nil {
		return 0, err
	}
	var identityID, userID int64
	for rows.Next() {
		var rowID, rowUser int64
		var storedUnionID string
		if err = rows.Scan(&rowID, &rowUser, &storedUnionID); err != nil {
			rows.Close()
			return 0, err
		}
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
		return 0, err
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
			return 0, err
		}
	}
	if userID == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO users(nickname,avatar_url) VALUES($1,$2) RETURNING id`, identity.Nickname, identity.AvatarURL).Scan(&userID)
		if err != nil {
			return 0, err
		}
	}
	if identityID == 0 {
		_, err = tx.Exec(ctx, `INSERT INTO identities(user_id,provider_id,issuer,subject,qq_unionid,nickname,avatar_url) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7)`, userID, identity.ProviderID, identity.Issuer, identity.Subject, identity.UnionID, identity.Nickname, identity.AvatarURL)
	} else {
		_, err = tx.Exec(ctx, `UPDATE identities SET issuer=$2,subject=$3,qq_unionid=COALESCE(NULLIF($4,''),qq_unionid),nickname=$5,avatar_url=$6 WHERE id=$1`, identityID, identity.Issuer, identity.Subject, identity.UnionID, identity.Nickname, identity.AvatarURL)
	}
	var constraint *pgconn.PgError
	if errors.As(err, &constraint) && constraint.Code == "23505" {
		return 0, ErrConflict
	}
	if err != nil {
		return 0, err
	}
	if bindUserID == 0 {
		if _, err = tx.Exec(ctx, `UPDATE users SET nickname=$2,avatar_url=$3 WHERE id=$1`, userID, identity.Nickname, identity.AvatarURL); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return userID, nil
}
