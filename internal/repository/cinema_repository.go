package repository // repository contains data access logic separated from handlers

import (
	"context"      // context allows passing deadlines and cancellation signals to DB operations
	"database/sql" // sql provides generic database operations and drivers
	"errors"       // errors is used to define custom error values
)

// Cinema represents a cinema entity persisted in the database.  Each
// cinema belongs to a single owner and may contain multiple halls.  The
// ID field is the primary key and is auto-incremented by the DB.
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
// ID field will be populated with the auto-generated value.  سپس رکورد
// ایجادشده خوانده می‌شود تا CreatedAt/UpdatedAt هم پر شوند.
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

	// فیلدهای زمانی را هم با یک SELECT پر می‌کنیم تا پاسخ API کامل باشد.
	const qSelect = "SELECT owner_id, name, created_at, updated_at FROM cinemas WHERE id = ?"
	if err := r.db.QueryRowContext(ctx, qSelect, c.ID).
		Scan(&c.OwnerID, &c.Name, &c.CreatedAt, &c.UpdatedAt); err != nil {
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
