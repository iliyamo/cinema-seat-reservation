// Package repository contains data access logic separated from HTTP handlers.
// This file defines the Cinema model and repository methods for CRUD and lookup
// operations. A Cinema represents a venue that can contain multiple halls.
// Only minimal fields (ID and Name) should be exposed in public API responses.
package repository

import (
	"context"      // context allows passing deadlines and cancellation signals to DB operations
	"database/sql" // sql provides generic database operations and drivers
	"errors"       // errors is used to define custom error values
)

// Cinema represents a cinema entity persisted in the database. Each cinema belongs to a single owner
// and may contain multiple halls. The ID field is the primary key and is auto-incremented by the DB.
// Note: OwnerID, CreatedAt and UpdatedAt should not be exposed via public API responses.
type Cinema struct {
	ID        uint64 // ID is the unique identifier of the cinema
	OwnerID   uint64 // OwnerID references the users.id of the cinema owner
	Name      string // Name is the human-friendly name of the cinema
	CreatedAt string // CreatedAt stores when the row was created (timestamp in DB timezone)
	UpdatedAt string // UpdatedAt stores when the row was last updated
}

// ErrCinemaNotFound is returned when a cinema cannot be found in the DB.
var ErrCinemaNotFound = errors.New("cinema not found")

// CinemaRepo encapsulates all database queries related to cinemas.  It
// depends on a sql.DB connection which should be configured elsewhere.
type CinemaRepo struct {
	db *sql.DB // db is the underlying database connection pool
}

// NewCinemaRepo constructs a CinemaRepo with the provided DB handle.  This
// function allows dependency injection of the database in tests and at
// startup.  There is no initialization logic beyond assigning the field.
func NewCinemaRepo(db *sql.DB) *CinemaRepo {
	return &CinemaRepo{db: db}
}

// Create inserts a new cinema into the database.  On success the cinema's
// ID field will be populated with the auto‑generated value.  After the
// insert, a SELECT is executed to populate the CreatedAt and UpdatedAt
// fields so that callers receive a fully populated record.
func (r *CinemaRepo) Create(ctx context.Context, c *Cinema) error {
	const qInsert = "INSERT INTO cinemas (owner_id, name) VALUES (?, ?)"
	res, err := r.db.ExecContext(ctx, qInsert, c.OwnerID, c.Name)
	if err != nil {
		return err // propagate DB errors to the caller
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	c.ID = uint64(id)

    // Perform a follow‑up SELECT to populate default timestamp fields (created_at, updated_at).
    const qSelect = "SELECT owner_id, name, created_at, updated_at FROM cinemas WHERE id = ?"
    if err := r.db.QueryRowContext(ctx, qSelect, c.ID).Scan(&c.OwnerID, &c.Name, &c.CreatedAt, &c.UpdatedAt); err != nil {
        return err
    }
    return nil
}

// GetByID fetches a cinema by its ID regardless of owner.  It returns
// ErrCinemaNotFound if no row is found.  Callers can use this method
// when they don't need to enforce ownership in the repository layer.
func (r *CinemaRepo) GetByID(ctx context.Context, id uint64) (*Cinema, error) {
	const q = "SELECT id, owner_id, name, created_at, updated_at FROM cinemas WHERE id = ?"
	var c Cinema
	if err := r.db.QueryRowContext(ctx, q, id).Scan(&c.ID, &c.OwnerID, &c.Name, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCinemaNotFound
		}
		return nil, err
	}
	return &c, nil
}

// GetByIDAndOwner fetches a cinema by id but only if it belongs to the
// specified owner.  If the cinema doesn't exist or is owned by someone
// else, ErrCinemaNotFound is returned.
func (r *CinemaRepo) GetByIDAndOwner(ctx context.Context, id, ownerID uint64) (*Cinema, error) {
	const q = "SELECT id, owner_id, name, created_at, updated_at FROM cinemas WHERE id = ? AND owner_id = ?"
	var c Cinema
	if err := r.db.QueryRowContext(ctx, q, id, ownerID).Scan(&c.ID, &c.OwnerID, &c.Name, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCinemaNotFound
		}
		return nil, err
	}
	return &c, nil
}

// ListByOwner returns all cinemas for a specific owner ordered by id.
func (r *CinemaRepo) ListByOwner(ctx context.Context, ownerID uint64) ([]*Cinema, error) {
	const q = `SELECT id, owner_id, name, created_at, updated_at
	           FROM cinemas WHERE owner_id = ? ORDER BY id`
	rows, err := r.db.QueryContext(ctx, q, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Cinema
	for rows.Next() {
		c := new(Cinema)
		if err := rows.Scan(&c.ID, &c.OwnerID, &c.Name, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateName updates the cinema name if it belongs to the provided owner.
// It returns sql.ErrNoRows when no row is affected (not found / not owned).
func (r *CinemaRepo) UpdateName(ctx context.Context, id, ownerID uint64, name string) error {
	const q = `UPDATE cinemas
	           SET name = ?, updated_at = CURRENT_TIMESTAMP
	           WHERE id = ? AND owner_id = ?`
	res, err := r.db.ExecContext(ctx, q, name, id, ownerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListAll returns all cinemas regardless of owner. It is used for public browsing
// endpoints to present available cinemas to unauthenticated users. Only ID and
// Name fields are selected to avoid exposing sensitive owner or timestamp fields.
func (r *CinemaRepo) ListAll(ctx context.Context) ([]*Cinema, error) {
    const q = `SELECT id, name FROM cinemas ORDER BY id`
    rows, err := r.db.QueryContext(ctx, q)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []*Cinema
    for rows.Next() {
        c := &Cinema{}
        if err := rows.Scan(&c.ID, &c.Name); err != nil {
            return nil, err
        }
        out = append(out, c)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return out, nil
}

// DeleteByIDAndOwner removes a cinema and all dependent records (halls, seats,
// shows, show seats, reservations and reservation seats) provided it belongs
// to the specified owner. If the cinema does not exist, sql.ErrNoRows is
// returned. If the cinema exists but is owned by a different user, ErrForbidden
// is returned. The deletion occurs within a transaction to maintain integrity.
func (r *CinemaRepo) DeleteByIDAndOwner(ctx context.Context, id, ownerID uint64) error {
    tx, err := r.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer func() {
        if err != nil {
            _ = tx.Rollback()
        } else {
            _ = tx.Commit()
        }
    }()
    // Verify cinema exists and ownership
    var dbOwnerID uint64
    if err = tx.QueryRowContext(ctx, `SELECT owner_id FROM cinemas WHERE id = ?`, id).Scan(&dbOwnerID); err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return sql.ErrNoRows
        }
        return err
    }
    if dbOwnerID != ownerID {
        return ErrForbidden
    }
    // Cascade delete: remove reservation_seats for shows in halls belonging to this cinema
    if _, err = tx.ExecContext(ctx,
        `DELETE rs FROM reservation_seats rs
         JOIN shows sh ON sh.id = rs.show_id
         JOIN halls h ON h.id = sh.hall_id
         WHERE h.cinema_id = ?`, id);
    err != nil {
        return err
    }
    // Delete reservations for shows in this cinema's halls
    if _, err = tx.ExecContext(ctx,
        `DELETE r FROM reservations r
         JOIN shows sh ON sh.id = r.show_id
         JOIN halls h ON h.id = sh.hall_id
         WHERE h.cinema_id = ?`, id);
    err != nil {
        return err
    }
    // Delete show_seats entries for shows in this cinema's halls
    if _, err = tx.ExecContext(ctx,
        `DELETE ss FROM show_seats ss
         JOIN shows sh ON sh.id = ss.show_id
         JOIN halls h ON h.id = sh.hall_id
         WHERE h.cinema_id = ?`, id);
    err != nil {
        return err
    }
    // Delete shows for halls in this cinema
    if _, err = tx.ExecContext(ctx,
        `DELETE sh FROM shows sh
         JOIN halls h ON h.id = sh.hall_id
         WHERE h.cinema_id = ?`, id);
    err != nil {
        return err
    }
    // Delete seats for halls in this cinema
    if _, err = tx.ExecContext(ctx,
        `DELETE s FROM seats s
         JOIN halls h ON h.id = s.hall_id
         WHERE h.cinema_id = ?`, id);
    err != nil {
        return err
    }
    // Delete halls for this cinema
    if _, err = tx.ExecContext(ctx, `DELETE FROM halls WHERE cinema_id = ?`, id); err != nil {
        return err
    }
    // Finally delete the cinema
    if _, err = tx.ExecContext(ctx, `DELETE FROM cinemas WHERE id = ?`, id); err != nil {
        return err
    }
    return nil
}
