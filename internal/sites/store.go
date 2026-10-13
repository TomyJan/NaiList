package sites

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"nailist/internal/logging"
)

type Store struct{ DB *pgxpool.Pool }

func (s Store) List(ctx context.Context, owner int64) ([]Site, error) {
	ctx = logging.WithOperation(ctx, "sites.select")
	rows, err := s.DB.Query(ctx, `SELECT id,owner_id,name,domain,organization,updated_at FROM sites WHERE deleted_at IS NULL AND ($1::bigint=0 OR owner_id=$1) ORDER BY updated_at DESC,id DESC`, owner)
	if err != nil {
		return nil, fmt.Errorf("读取有效站点列表: %w", err)
	}
	defer rows.Close()
	out := []Site{}
	for rows.Next() {
		var site Site
		if err := rows.Scan(&site.ID, &site.OwnerID, &site.Name, &site.Domain, &site.Organization, &site.UpdatedAt); err != nil {
			return nil, fmt.Errorf("读取站点记录: %w", err)
		}
		out = append(out, site)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历站点列表: %w", err)
	}
	return out, nil
}
func (s Store) Save(ctx context.Context, owner, id int64, site Site) (Site, error) {
	operation := "sites.insert"
	if id != 0 {
		operation = "sites.update"
	}
	ctx = logging.WithOperation(ctx, operation)
	var row pgx.Row
	if id == 0 {
		row = s.DB.QueryRow(ctx, `INSERT INTO sites(owner_id,name,domain,organization) VALUES($1,$2,$3,$4) RETURNING id,updated_at`, owner, site.Name, site.Domain, site.Organization)
	} else {
		row = s.DB.QueryRow(ctx, `UPDATE sites SET name=$3,domain=$4,organization=$5,updated_at=now() WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL RETURNING id,updated_at`, id, owner, site.Name, site.Domain, site.Organization)
	}
	err := row.Scan(&site.ID, &site.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return site, ErrNotFound
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return site, ErrDuplicate
	}
	if errors.As(err, &pe) && pe.Code == "23503" && pe.ConstraintName == "sites_owner_id_fkey" {
		return site, ErrNotFound
	}
	if err != nil {
		return site, fmt.Errorf("保存站点数据库记录 (%s): %w", operation, err)
	}
	site.OwnerID = owner
	return site, nil
}
func (s Store) Delete(ctx context.Context, owner, id int64) error {
	ctx = logging.WithOperation(ctx, "sites.soft_delete")
	tag, err := s.DB.Exec(ctx, `UPDATE sites SET deleted_at=now(),updated_at=now() WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL`, id, owner)
	if err != nil {
		return fmt.Errorf("软删除站点记录: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
