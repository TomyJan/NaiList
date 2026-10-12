package admin

import (
	"context"
	"fmt"
)

type DailyStats struct {
	Date   string `json:"date"`
	Users  int64  `json:"users"`
	Groups int64  `json:"groups"`
}
type Stats struct {
	Users         int64        `json:"users"`
	Groups        int64        `json:"groups"`
	Contributors  int64        `json:"contributors"`
	Organizations int64        `json:"organizations"`
	Daily         []DailyStats `json:"daily"`
}

// Stats uses one snapshot for totals and the zero-filled Beijing calendar.
// Trends describe the creation dates of records that still exist and are active.
func (s Store) Stats(ctx context.Context) (Stats, error) {
	rows, err := s.DB.Query(ctx, `
WITH bounds AS (
    SELECT (now() AT TIME ZONE 'Asia/Shanghai')::date AS today
), calendar AS (
    SELECT today - offset_day AS day FROM bounds CROSS JOIN generate_series(0,29) AS offset_day
), user_days AS (
    SELECT (created_at AT TIME ZONE 'Asia/Shanghai')::date AS day, count(*) AS count
    FROM users, bounds
    WHERE created_at >= ((today - 29)::timestamp AT TIME ZONE 'Asia/Shanghai')
      AND created_at < ((today + 1)::timestamp AT TIME ZONE 'Asia/Shanghai')
    GROUP BY 1
), group_days AS (
    SELECT (created_at AT TIME ZONE 'Asia/Shanghai')::date AS day, count(*) AS count
    FROM groups, bounds
    WHERE deleted_at IS NULL
      AND created_at >= ((today - 29)::timestamp AT TIME ZONE 'Asia/Shanghai')
      AND created_at < ((today + 1)::timestamp AT TIME ZONE 'Asia/Shanghai')
    GROUP BY 1
), totals AS (
    SELECT (SELECT count(*) FROM users) AS users, count(*) AS groups,
        count(DISTINCT owner_id) AS contributors, count(DISTINCT organization) AS organizations
    FROM groups WHERE deleted_at IS NULL
)
SELECT totals.users, totals.groups, totals.contributors, totals.organizations,
    to_char(calendar.day,'YYYY-MM-DD'), COALESCE(user_days.count,0), COALESCE(group_days.count,0)
FROM calendar CROSS JOIN totals
LEFT JOIN user_days ON user_days.day=calendar.day
LEFT JOIN group_days ON group_days.day=calendar.day
ORDER BY calendar.day`)
	if err != nil {
		return Stats{}, fmt.Errorf("query admin statistics: %w", err)
	}
	defer rows.Close()
	out := Stats{Daily: make([]DailyStats, 0, 30)}
	for rows.Next() {
		var day DailyStats
		if err := rows.Scan(&out.Users, &out.Groups, &out.Contributors, &out.Organizations, &day.Date, &day.Users, &day.Groups); err != nil {
			return Stats{}, fmt.Errorf("scan admin statistics: %w", err)
		}
		out.Daily = append(out.Daily, day)
	}
	if err := rows.Err(); err != nil {
		return Stats{}, fmt.Errorf("read admin statistics: %w", err)
	}
	return out, nil
}
