package admin

import (
	"context"
	"errors"
	"fmt"
	"nailist/internal/sites"
	"testing"
)

func TestPostgresSitesTransferAndDeleteDestinationSerialize(t *testing.T) {
	db := testDB(t)
	store := Store{DB: db}
	ctx := context.Background()
	source := user(t, db)
	for i := 0; i < 12; i++ {
		target := user(t, db)
		domain := fmt.Sprintf("site%d.example.com", i)
		site, err := (sites.Store{DB: db}).Save(ctx, source, 0, sites.Site{Name: "old", Domain: domain, Organization: "old"})
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		deleted := make(chan error, 1)
		transferred := make(chan error, 1)
		go func() { <-start; _, err := store.DeleteUser(ctx, source, target); deleted <- err }()
		go func() {
			<-start
			_, _, err := store.SaveSite(ctx, site.ID, target, sites.Site{Name: "new", Domain: domain, Organization: "new"})
			transferred <- err
		}()
		close(start)
		de, te := <-deleted, <-transferred
		if de == nil {
			if !errors.Is(te, ErrNotFound) {
				t.Fatalf("deleted destination accepted: %v", te)
			}
		} else if !errors.Is(de, ErrActiveSites) || te != nil {
			t.Fatalf("transfer/delete: %v / %v", te, de)
		}
	}
}

func TestPostgresSitesCountsAndAtomicTransfer(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	s := Store{DB: db}
	a, b := user(t, db), user(t, db)
	first, err := (sites.Store{DB: db}).Save(ctx, a, 0, sites.Site{Name: "first", Domain: "first.example.com", Organization: "shared"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := (sites.Store{DB: db}).Save(ctx, b, 0, sites.Site{Name: "second", Domain: "second.example.com", Organization: "site only"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO groups(owner_id,organization,qq_number,join_url) VALUES($1,'shared','12345','https://qm.qq.com/q/test')`, a); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.SaveSite(ctx, first.ID, b, sites.Site{Name: "changed", Domain: second.Domain, Organization: "changed"}); !errors.Is(err, sites.ErrDuplicate) {
		t.Fatal(err)
	}
	current, err := (sites.Store{DB: db}).List(ctx, a)
	if err != nil || len(current) != 1 || current[0].Name != "first" {
		t.Fatalf("failed transfer modified fields/owner: %+v %v", current, err)
	}
	users, err := s.Users(ctx)
	if err != nil || len(users) != 2 || users[0].SiteCount != 1 || users[1].SiteCount != 1 {
		t.Fatalf("users: %+v %v", users, err)
	}
	stats, err := s.Stats(ctx)
	if err != nil || stats.Sites != 2 || stats.Groups != 1 || stats.Contributors != 2 || stats.Organizations != 2 || stats.Daily[29].Sites != 2 {
		t.Fatalf("stats: %+v %v", stats, err)
	}
	if err := (sites.Store{DB: db}).Delete(ctx, b, second.ID); err != nil {
		t.Fatal(err)
	}
	stats, err = s.Stats(ctx)
	if err != nil || stats.Sites != 1 || stats.Contributors != 1 || stats.Organizations != 1 || stats.Daily[29].Sites != 1 {
		t.Fatalf("active stats: %+v %v", stats, err)
	}
	cleanup, err := s.DeleteUser(ctx, a, b)
	if err != nil || cleanup.DeletedSites != 1 {
		t.Fatalf("history cleanup: %+v %v", cleanup, err)
	}
	if _, err := (sites.Store{DB: db}).Save(ctx, b, 0, sites.Site{Name: "deleted owner", Domain: "deleted.example.com", Organization: "x"}); !errors.Is(err, sites.ErrNotFound) {
		t.Fatal(err)
	}
}
