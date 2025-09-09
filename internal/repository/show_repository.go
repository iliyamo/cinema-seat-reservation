// Package repository contains data access logic for Show domain operations. This file defines
// the Show model and repository methods for shows. A Show represents a scheduled
// screening of a movie in a hall. Sensitive fields such as BasePriceCents,
// Status, CreatedAt and UpdatedAt should not be exposed via public API responses.
package repository

import (
	"context"      // context for controlling query lifetime
	"database/sql" // sql provides DB abstraction
	"errors"       // errors for sentinel definitions
)

// Show represents a scheduled screening of a movie in a particular hall.
// StartsAt and EndsAt define the schedule; BasePriceCents is the default
// price for seats unless overridden per seat.
// NOTE: Time strings are stored in DB format "2006-01-02 15:04:05" (UTC).
type Show struct {
	ID             uint64 // ID is the primary key of the show
	HallID         uint64 // HallID references the hall where the show occurs
	Title          string // Title is the name of the movie or event
	StartsAt       string // StartsAt is the DB timestamp when the show begins ("YYYY-MM-DD HH:MM:SS" UTC)
	EndsAt         string // EndsAt is the DB timestamp when the show ends   ("YYYY-MM-DD HH:MM:SS" UTC)
	BasePriceCents uint32 // BasePriceCents is the base price for a seat in cents
	Status         string // Status is the state of the show (SCHEDULED, CANCELLED, FINISHED)
	CreatedAt      string // CreatedAt records row creation time
	UpdatedAt      string // UpdatedAt records last update time
}

// ErrShowNotFound indicates that a show was not located in the DB.
var ErrShowNotFound = errors.New("show not found")

// ErrNoChange indicates the UPDATE attempted to set fields equal to current values.
var ErrNoChange = errors.New("no change")

// ShowRepo manages persistence for shows.
type ShowRepo struct {
	db *sql.DB
}

// DB exposes the underlying sql.DB.  It allows callers to begin
// transactions spanning multiple repositories.  Use this method to
// obtain a *sql.DB when you need fine-grained transaction control.
func (r *ShowRepo) DB() *sql.DB {
    return r.db
}

// CreateTx inserts a new show using the provided transaction instead of
// the repository's DB handle.  It behaves like Create but does not
// commit the transaction.  The caller must commit or roll back the
// transaction.  On success, the generated ID and DB-default fields
// (status, created_at, updated_at) are populated on the given Show.
func (r *ShowRepo) CreateTx(ctx context.Context, tx *sql.Tx, s *Show) error {
    const q = `INSERT INTO shows (hall_id, title, starts_at, ends_at, base_price_cents) VALUES (?, ?, ?, ?, ?)`
    // Execute the insert using the provided transaction. Do not use
    // r.db here to ensure the operation participates in the caller's
    // transaction.
    res, err := tx.ExecContext(ctx, q, s.HallID, s.Title, s.StartsAt, s.EndsAt, s.BasePriceCents)
    if err != nil {
        return err
    }
    // Retrieve the auto-incremented ID assigned by the database.
    id, err := res.LastInsertId()
    if err != nil {
        return err
    }
    s.ID = uint64(id)
    // Query the inserted row to obtain default fields such as status and timestamps.
    const sel = `SELECT id, hall_id, title, starts_at, ends_at, base_price_cents, status, created_at, updated_at
                 FROM shows WHERE id = ?`
    return tx.QueryRowContext(ctx, sel, s.ID).Scan(
        &s.ID,
        &s.HallID,
        &s.Title,
        &s.StartsAt,
        &s.EndsAt,
        &s.BasePriceCents,
        &s.Status,
        &s.CreatedAt,
        &s.UpdatedAt,
    )
}

// NewShowRepo constructs a ShowRepo with the given DB handle.
func NewShowRepo(db *sql.DB) *ShowRepo {
	return &ShowRepo{db: db}
}

// Create inserts a new show into the database and assigns the generated
// ID back to the show struct.  The caller must provide hall_id,
// title, starts_at and ends_at.  BasePriceCents can be optionally
// supplied; if zero the DB default of 0 will be used.  Status is
// implicitly SCHEDULED by the DB.
func (r *ShowRepo) Create(ctx context.Context, s *Show) error {
	const q = `INSERT INTO shows (hall_id, title, starts_at, ends_at, base_price_cents) VALUES (?, ?, ?, ?, ?)` // SQL insert for shows
	res, err := r.db.ExecContext(ctx, q, s.HallID, s.Title, s.StartsAt, s.EndsAt, s.BasePriceCents)             // execute insertion
	if err != nil {                                                                                             // check execution error
		return err // propagate the error
	}
	id, err := res.LastInsertId() // obtain the auto-incremented ID
	if err != nil {               // check error retrieving ID
		return err // propagate error
	}
	s.ID = uint64(id) // assign the generated ID to the show model
	// Fetch the freshly inserted row to populate default fields (status, created_at, updated_at)
	const sel = `SELECT id, hall_id, title, starts_at, ends_at, base_price_cents, status, created_at, updated_at FROM shows WHERE id = ?` // select query
	err = r.db.QueryRowContext(ctx, sel, s.ID).Scan(                                                                                      // scan the selected row into the struct
		&s.ID, &s.HallID, &s.Title, &s.StartsAt, &s.EndsAt, &s.BasePriceCents, &s.Status, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil { // check scanning error
		return err // propagate error
	}
	return nil // return nil on success
}

// GetByID retrieves a show by its ID.  It returns ErrShowNotFound if
// there is no matching row.
func (r *ShowRepo) GetByID(ctx context.Context, id uint64) (*Show, error) {
	const q = `SELECT id, hall_id, title, starts_at, ends_at, base_price_cents, status, created_at, updated_at FROM shows WHERE id = ?`
	var s Show
	err := r.db.QueryRowContext(ctx, q, id).Scan(&s.ID, &s.HallID, &s.Title, &s.StartsAt, &s.EndsAt, &s.BasePriceCents, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrShowNotFound
		}
		return nil, err
	}
	return &s, nil
}

// ListByHallAndOwner returns all shows for a given hall that belong to the specified owner.
// The owner constraint is enforced via the halls table.  Results are ordered by start
// time ascending.  When no shows exist it returns an empty slice and nil error.
func (r *ShowRepo) ListByHallAndOwner(ctx context.Context, hallID, ownerID uint64) ([]Show, error) {
	// Select shows joined with halls to check owner_id on halls.  Only select shows for
	// the requested hall and owner.
	const q = `SELECT s.id, s.hall_id, s.title, s.starts_at, s.ends_at, s.base_price_cents, s.status, s.created_at, s.updated_at
               FROM shows s
               JOIN halls h ON h.id = s.hall_id
               WHERE s.hall_id = ? AND h.owner_id = ?
               ORDER BY s.starts_at ASC`
	rows, err := r.db.QueryContext(ctx, q, hallID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Show
	for rows.Next() {
		var s Show
		if err := rows.Scan(
			&s.ID, &s.HallID, &s.Title, &s.StartsAt, &s.EndsAt, &s.BasePriceCents, &s.Status, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// ListByHall returns all shows for a given hall regardless of owner. It is used by
// public browse endpoints to display available shows to unauthenticated users. Shows
// are ordered by their start time ascending.
func (r *ShowRepo) ListByHall(ctx context.Context, hallID uint64) ([]Show, error) {
    const q = `SELECT s.id, s.hall_id, s.title, s.starts_at, s.ends_at, s.base_price_cents, s.status, s.created_at, s.updated_at
               FROM shows s
               WHERE s.hall_id = ?
               ORDER BY s.starts_at ASC`
    rows, err := r.db.QueryContext(ctx, q, hallID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var result []Show
    for rows.Next() {
        var s Show
        if err := rows.Scan(
            &s.ID, &s.HallID, &s.Title, &s.StartsAt, &s.EndsAt,
            &s.BasePriceCents, &s.Status, &s.CreatedAt, &s.UpdatedAt,
        ); err != nil {
            return nil, err
        }
        result = append(result, s)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return result, nil
}

// FindOverlapping finds all shows in the specified hall whose scheduled time overlaps
// the provided interval [start, end).  A show overlaps when it starts before the
// proposed end and ends after the proposed start.  Time strings must use the same
// format as stored in the database ("2006-01-02 15:04:05").  It returns an empty
// slice when no overlaps are found.
func (r *ShowRepo) FindOverlapping(ctx context.Context, hallID uint64, start, end string) ([]Show, error) {
	// Use a predicate that selects shows where NOT (existing ends before new starts OR existing starts after new ends).
	const q = `SELECT id, hall_id, title, starts_at, ends_at, base_price_cents, status, created_at, updated_at
               FROM shows
               WHERE hall_id = ? AND NOT (ends_at <= ? OR starts_at >= ?)`
	rows, err := r.db.QueryContext(ctx, q, hallID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var overlaps []Show
	for rows.Next() {
		var s Show
		if err := rows.Scan(
			&s.ID, &s.HallID, &s.Title, &s.StartsAt, &s.EndsAt, &s.BasePriceCents, &s.Status, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		overlaps = append(overlaps, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return overlaps, nil
}

// FindOverlappingExcluding is similar to FindOverlapping but excludes the show with the given ID
// from the overlap check.  This is used during updates to allow a show to overlap with itself.
func (r *ShowRepo) FindOverlappingExcluding(ctx context.Context, hallID, excludeID uint64, start, end string) ([]Show, error) {
	const q = `SELECT id, hall_id, title, starts_at, ends_at, base_price_cents, status, created_at, updated_at
               FROM shows
               WHERE hall_id = ? AND id <> ? AND NOT (ends_at <= ? OR starts_at >= ?)`
	rows, err := r.db.QueryContext(ctx, q, hallID, excludeID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var overlaps []Show
	for rows.Next() {
		var s Show
		if err := rows.Scan(
			&s.ID, &s.HallID, &s.Title, &s.StartsAt, &s.EndsAt, &s.BasePriceCents, &s.Status, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		overlaps = append(overlaps, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return overlaps, nil
}

// UpdateByIDAndOwner updates a show's attributes if it belongs to a hall owned by the given owner.
// It only performs the UPDATE when there is at least one differing field;
// otherwise it returns ErrNoChange. When the row/ownership doesn't match,
// it returns sql.ErrNoRows.
func (r *ShowRepo) UpdateByIDAndOwner(ctx context.Context, s *Show, ownerID uint64) error {
	const q = `UPDATE shows sh
               JOIN halls h ON h.id = sh.hall_id
               SET sh.title = ?, sh.starts_at = ?, sh.ends_at = ?, sh.base_price_cents = ?, sh.status = ?, sh.updated_at = CURRENT_TIMESTAMP
               WHERE sh.id = ? AND h.owner_id = ?
                 AND (sh.title <> ? OR sh.starts_at <> ? OR sh.ends_at <> ? OR sh.base_price_cents <> ? OR sh.status <> ?)`

	res, err := r.db.ExecContext(ctx, q,
		s.Title, s.StartsAt, s.EndsAt, s.BasePriceCents, s.Status, // SET
		s.ID, ownerID, // WHERE (record + owner)
		s.Title, s.StartsAt, s.EndsAt, s.BasePriceCents, s.Status, // only if at least one field differs
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}

	// Determine if it's "not found/ownership" or simply "no change".
	const qExists = `SELECT 1
                     FROM shows sh
                     JOIN halls h ON h.id = sh.hall_id
                     WHERE sh.id = ? AND h.owner_id = ?
                     LIMIT 1`
	var one int
	if err := r.db.QueryRowContext(ctx, qExists, s.ID, ownerID).Scan(&one); err != nil {
		if err == sql.ErrNoRows {
			return sql.ErrNoRows // record doesn't exist or belongs to another owner
		}
		return err
	}
	return ErrNoChange // row exists but values are identical
}

// DeleteByIDAndOwner removes a show and all of its dependent records provided the
// show belongs to a hall owned by the given owner. The deletion occurs within
// a transaction to ensure that no partial cleanup occurs. If the show does
// not exist, ErrShowNotFound is returned. If it is owned by another user,
// ErrForbidden is returned. If any reservations exist for the show, the
// deletion is aborted and ErrConflict is returned.
func (r *ShowRepo) DeleteByIDAndOwner(ctx context.Context, id, ownerID uint64) error {
    tx, err := r.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    // Ensure rollback or commit at the end
    defer func() {
        if err != nil {
            _ = tx.Rollback()
        } else {
            _ = tx.Commit()
        }
    }()
    // Verify show exists and belongs to the specified owner
    var dbOwnerID uint64
    err = tx.QueryRowContext(ctx,
        `SELECT h.owner_id FROM shows sh JOIN halls h ON h.id = sh.hall_id WHERE sh.id = ?`, id,
    ).Scan(&dbOwnerID)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return ErrShowNotFound
        }
        return err
    }
    if dbOwnerID != ownerID {
        return ErrForbidden
    }
    // Check for existing reservations referencing this show
    var resCount int
    if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM reservations WHERE show_id = ?`, id).Scan(&resCount); err != nil {
        return err
    }
    if resCount > 0 {
        return ErrConflict
    }
    // Remove reservation_seats associated with the show (should be none if resCount == 0, but defensive)
    if _, err = tx.ExecContext(ctx, `DELETE FROM reservation_seats WHERE show_id = ?`, id); err != nil {
        return err
    }
    // Remove show seats entries for the show
    if _, err = tx.ExecContext(ctx, `DELETE FROM show_seats WHERE show_id = ?`, id); err != nil {
        return err
    }
    // Delete the show itself
    if _, err = tx.ExecContext(ctx, `DELETE FROM shows WHERE id = ?`, id); err != nil {
        return err
    }
    return nil
}
