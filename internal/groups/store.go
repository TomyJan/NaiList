package groups

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ DB *pgxpool.Pool }

func (s Store) List(ctx context.Context, owner int64) ([]Group, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,owner_id,organization,keywords,qq_number,join_url,updated_at FROM groups WHERE deleted_at IS NULL AND ($1::bigint=0 OR owner_id=$1) ORDER BY updated_at DESC,id DESC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.OwnerID, &g.Organization, &g.Keywords, &g.QQNumber, &g.JoinURL, &g.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (s Store) Save(ctx context.Context, owner, id int64, g Group) (Group, error) {
	var row pgx.Row
	if id == 0 {
		row = s.DB.QueryRow(ctx, `INSERT INTO groups(owner_id,organization,keywords,qq_number,join_url) VALUES($1,$2,$3,$4,$5) RETURNING id,updated_at`, owner, g.Organization, g.Keywords, g.QQNumber, g.JoinURL)
	} else {
		row = s.DB.QueryRow(ctx, `UPDATE groups SET organization=$3,keywords=$4,qq_number=$5,join_url=$6,updated_at=now() WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL RETURNING id,updated_at`, id, owner, g.Organization, g.Keywords, g.QQNumber, g.JoinURL)
	}
	err := row.Scan(&g.ID, &g.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrNotFound
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return g, ErrDuplicate
	}
	g.OwnerID = owner
	return g, err
}
func (s Store) Delete(ctx context.Context, owner, id int64) error {
	tag, err := s.DB.Exec(ctx, `UPDATE groups SET deleted_at=now(),updated_at=now() WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL`, id, owner)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
