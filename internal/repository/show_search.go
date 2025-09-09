package repository

import (
	"context"
	"strings"
)

// ShowSearchQuery defines filters & pagination for searching shows.
type ShowSearchQuery struct {
	Title      string
	Cinema     string
	Hall       string
	TimeFilter string
	Page       int
	PageSize   int
}

type PublicShowRow struct {
	ID         uint64  `json:"id"`
	Title      string  `json:"title"`
	HallID     uint64  `json:"hall_id"`
	HallName   string  `json:"hall_name"`
	CinemaID   uint64  `json:"cinema_id"`
	Cinema     string  `json:"cinema"`
	StartsAt   string  `json:"starts_at"`
	EndsAt     string  `json:"ends_at"`
	PriceCents uint64  `json:"price_cents"`
	Price      float64 `json:"price"`
}

func (r *ShowRepo) SearchUpcoming(ctx context.Context, q ShowSearchQuery) ([]PublicShowRow, int64, error) {
	where := []string{}
	args := []any{}

	switch strings.ToLower(q.TimeFilter) {
	case "any":
	case "active":
		where = append(where, "s.ends_at >= NOW()")
	default:
		where = append(where, "s.starts_at >= NOW()")
	}

	if q.Title != "" {
		where = append(where, "LOWER(s.title) LIKE ?")
		args = append(args, "%"+strings.ToLower(q.Title)+"%")
	}
	if q.Cinema != "" {
		where = append(where, "LOWER(c.name) LIKE ?")
		args = append(args, "%"+strings.ToLower(q.Cinema)+"%")
	}
	if q.Hall != "" {
		where = append(where, "LOWER(h.name) LIKE ?")
		args = append(args, "%"+strings.ToLower(q.Hall)+"%")
	}

	cond := "1=1"
	if len(where) > 0 {
		cond = strings.Join(where, " AND ")
	}

	var total int64
	countSQL := `SELECT COUNT(*)
		FROM shows s
		JOIN halls h   ON h.id = s.hall_id
		JOIN cinemas c ON c.id = h.cinema_id
		WHERE ` + cond
	if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := q.PageSize
	offset := (q.Page - 1) * q.PageSize

	dataSQL := `SELECT
			s.id,
			s.title,
			s.hall_id,
			h.name AS hall_name,
			c.id   AS cinema_id,
			c.name AS cinema_name,
			DATE_FORMAT(s.starts_at, '%Y-%m-%d %T') AS starts_at,
			DATE_FORMAT(s.ends_at,   '%Y-%m-%d %T') AS ends_at,
			COALESCE(s.base_price_cents, 0) AS price_cents
		FROM shows s
		JOIN halls h   ON h.id = s.hall_id
		JOIN cinemas c ON c.id = h.cinema_id
		WHERE ` + cond + `
		ORDER BY s.starts_at ASC
		LIMIT ? OFFSET ?`

	argsData := append(append([]any{}, args...), limit, offset)

	rows, err := r.db.QueryContext(ctx, dataSQL, argsData...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]PublicShowRow, 0, limit)
	for rows.Next() {
		var d PublicShowRow
		if err := rows.Scan(
			&d.ID,
			&d.Title,
			&d.HallID,
			&d.HallName,
			&d.CinemaID,
			&d.Cinema,
			&d.StartsAt,
			&d.EndsAt,
			&d.PriceCents,
		); err != nil {
			return nil, 0, err
		}
		d.Price = float64(d.PriceCents) / 100.0
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
