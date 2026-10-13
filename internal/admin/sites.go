package admin

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"nailist/internal/logging"
	"nailist/internal/sites"
)

type Site struct {
	sites.Site
	OwnerID int64 `json:"ownerId"`
}

func (s Store) Sites(ctx context.Context) ([]Site, error) {
	rows, err := (sites.Store{DB: s.DB}).List(ctx, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Site, 0, len(rows))
	for _, site := range rows {
		out = append(out, Site{site, site.OwnerID})
	}
	return out, nil
}
func (s Store) SaveSite(ctx context.Context, id, owner int64, site sites.Site) (Site, int64, error) {
	ctx = logging.WithOperation(ctx, "admin.sites.update")
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Site{}, 0, err
	}
	defer rollback(ctx, tx)
	// Account deletion takes the same destination lock before checking active resources.
	stage(ctx, "lock_owner", "site_id", id, "new_owner_id", owner)
	if err = lockUser(ctx, tx, owner); err != nil {
		return Site{}, 0, err
	}
	var old int64
	if err = tx.QueryRow(ctx, `SELECT owner_id FROM sites WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&old); errors.Is(err, pgx.ErrNoRows) {
		return Site{}, 0, sites.ErrNotFound
	} else if err != nil {
		return Site{}, 0, err
	}
	err = tx.QueryRow(ctx, `UPDATE sites SET owner_id=$2,name=$3,domain=$4,organization=$5,updated_at=now() WHERE id=$1 RETURNING updated_at`, id, owner, site.Name, site.Domain, site.Organization).Scan(&site.UpdatedAt)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return Site{}, 0, sites.ErrDuplicate
	}
	if err != nil {
		return Site{}, 0, err
	}
	site.ID, site.OwnerID = id, owner
	stage(ctx, "commit", "site_id", id, "old_owner_id", old, "new_owner_id", owner)
	if err = tx.Commit(ctx); err != nil {
		return Site{}, 0, err
	}
	return Site{site, owner}, old, nil
}
