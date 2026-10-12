package admin

import (
	"context"
	"testing"
)

func TestPostgresStatsCalendarAndActiveRecords(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	store := Store{DB: db}
	empty, err := store.Stats(ctx)
	if err != nil || empty.Users != 0 || empty.Groups != 0 || len(empty.Daily) != 30 {
		t.Fatalf("empty stats: %+v %v", empty, err)
	}
	var owner int64
	if err = db.QueryRow(ctx, `INSERT INTO users(nickname,created_at) VALUES('old',now()-interval '40 days') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO users(nickname,created_at) VALUES
        ('today',date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'),
        ('yesterday',(date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai')-interval '1 second'),
        ('first',(date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai')-interval '29 days') AT TIME ZONE 'Asia/Shanghai'),
        ('outside',(date_trunc('day',now() AT TIME ZONE 'Asia/Shanghai')-interval '29 days') AT TIME ZONE 'Asia/Shanghai'-interval '1 second')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO groups(owner_id,organization,qq_number,join_url,created_at,deleted_at) VALUES
        ($1,'大学A','12345','https://qm.qq.com/q/a',now(),NULL),
        ($1,'大学A','12346','https://qm.qq.com/q/b',now()-interval '40 days',NULL),
        ($1,'大学B','12347','https://qm.qq.com/q/c',now(),NULL),
        ($1,'已删','12348','https://qm.qq.com/q/d',now(),now())`, owner); err != nil {
		t.Fatal(err)
	}
	got, err := store.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Users != 5 || got.Groups != 3 || got.Contributors != 1 || got.Organizations != 2 || len(got.Daily) != 30 {
		t.Fatalf("counts: %+v", got)
	}
	if got.Daily[0].Users != 1 || got.Daily[28].Users != 1 || got.Daily[29].Users != 1 || got.Daily[29].Groups != 2 {
		t.Fatalf("calendar boundary: %+v", got.Daily)
	}
	var today string
	if err = db.QueryRow(ctx, `SELECT to_char(now() AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD')`).Scan(&today); err != nil {
		t.Fatal(err)
	}
	if got.Daily[29].Date != today {
		t.Fatalf("last day: %s != %s", got.Daily[29].Date, today)
	}
	for i := 1; i < 28; i++ {
		if got.Daily[i].Users != 0 || got.Daily[i].Groups != 0 || got.Daily[i].Date <= got.Daily[i-1].Date {
			t.Fatalf("zero-fill/order: %+v", got.Daily)
		}
	}
	if _, err = db.Exec(ctx, `UPDATE groups SET deleted_at=now() WHERE qq_number IN ('12345','12347'); DELETE FROM users WHERE nickname='today'`); err != nil {
		t.Fatal(err)
	}
	after, err := store.Stats(ctx)
	if err != nil || after.Users != 4 || after.Groups != 1 || after.Organizations != 1 || after.Daily[29].Users != 0 || after.Daily[29].Groups != 0 {
		t.Fatalf("deleted data counted: %+v %v", after, err)
	}

}
