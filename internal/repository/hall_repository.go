package repository // repository holds data access logic for domain entities

import (
	"context"      // context is used to manage deadlines and cancellation
	"database/sql" // sql provides DB primitives
	"errors"       // errors package allows sentinel error definitions
)

// Hall represents a screening hall within a cinema.  Each hall belongs to
// a cinema and an owner.  SeatRows and SeatCols describe the seat layout.
type Hall struct {
	ID          uint64         // ID is the primary key of the hall
	OwnerID     uint64         // OwnerID references the owning user's ID
	CinemaID    *uint64        // CinemaID references the parent cinema; nullable for backward compatibility
	Name        string         // Name is a human readable label for the hall
	Description sql.NullString // Description is optional text about the hall
	SeatRows    sql.NullInt32  // SeatRows indicates how many seating rows exist; nullable
	SeatCols    sql.NullInt32  // SeatCols indicates how many seats per row; nullable
	IsActive    bool           // IsActive flag indicates if the hall is currently in use
	CreatedAt   string         // CreatedAt stores creation timestamp
	UpdatedAt   string         // UpdatedAt stores last update timestamp
}

// ErrHallNotFound is returned when a hall lookup fails.
var ErrHallNotFound = errors.New("hall not found")

// HallRepo provides methods to create and retrieve halls.  It embeds a
// database handle to perform queries and commands.
type HallRepo struct {
	db *sql.DB // db is the underlying database connection
}

// NewHallRepo constructs a HallRepo with the given DB handle.
func NewHallRepo(db *sql.DB) *HallRepo {
	return &HallRepo{db: db}
}

// Create inserts a new hall into the database.  The hall must have
// OwnerID and Name set.  CinemaID, Rows and Cols may be nil to support
// old behaviour but should be provided for new halls.  After insert
// the ID field of the hall will be set. سپس رکورد خوانده می‌شود تا
// فیلدهای زمان و وضعیت هم پر شوند.
func (r *HallRepo) Create(ctx context.Context, h *Hall) error {
	const qInsert = `INSERT INTO halls (owner_id, cinema_id, name, description, seat_rows, seat_cols)
	                 VALUES (?, ?, ?, ?, ?, ?)`
	res, err := r.db.ExecContext(ctx, qInsert, h.OwnerID, h.CinemaID, h.Name, h.Description, h.SeatRows, h.SeatCols)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	h.ID = uint64(id)

	// رکورد را بخوان تا is_active, created_at, updated_at و مقادیر نهایی ست شوند.
	const qSelect = `SELECT id, owner_id, cinema_id, name, description, seat_rows, seat_cols, is_active, created_at, updated_at
	                 FROM halls WHERE id = ?`
	if err := r.db.QueryRowContext(ctx, qSelect, h.ID).
		Scan(&h.ID, &h.OwnerID, &h.CinemaID, &h.Name, &h.Description, &h.SeatRows, &h.SeatCols, &h.IsActive, &h.CreatedAt, &h.UpdatedAt); err != nil {
		return err
	}

	return nil
}

// GetByID retrieves a hall by its ID regardless of owner.  It returns
// ErrHallNotFound when no row is found.  Rows and Cols may come back
// NULL and are represented using sql.NullInt32.
func (r *HallRepo) GetByID(ctx context.Context, id uint64) (*Hall, error) {
	const q = `SELECT id, owner_id, cinema_id, name, description, seat_rows, seat_cols, is_active, created_at, updated_at FROM halls WHERE id = ?`
	var h Hall
	// Perform the query and scan results into the hall struct fields.
	err := r.db.QueryRowContext(ctx, q, id).Scan(&h.ID, &h.OwnerID, &h.CinemaID, &h.Name, &h.Description, &h.SeatRows, &h.SeatCols, &h.IsActive, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrHallNotFound
		}
		return nil, err
	}
	return &h, nil
}

// GetByIDAndOwner retrieves a hall but only if it belongs to the given
// owner.  This helper is used to enforce resource ownership.  If no
// matching hall is found, ErrHallNotFound is returned.
func (r *HallRepo) GetByIDAndOwner(ctx context.Context, id, ownerID uint64) (*Hall, error) {
	const q = `SELECT id, owner_id, cinema_id, name, description, seat_rows, seat_cols, is_active, created_at, updated_at FROM halls WHERE id = ? AND owner_id = ?`
	var h Hall
	err := r.db.QueryRowContext(ctx, q, id, ownerID).Scan(&h.ID, &h.OwnerID, &h.CinemaID, &h.Name, &h.Description, &h.SeatRows, &h.SeatCols, &h.IsActive, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrHallNotFound
		}
		return nil, err
	}
	return &h, nil
}

// ListByCinemaAndOwner returns all halls inside a cinema for the owner.
// Useful for GET /v1/cinemas/:cinema_id/halls.
func (r *HallRepo) ListByCinemaAndOwner(ctx context.Context, cinemaID, ownerID uint64) ([]*Hall, error) {
	const q = `SELECT id, owner_id, cinema_id, name, description, seat_rows, seat_cols, is_active, created_at, updated_at
               FROM halls
               WHERE cinema_id = ? AND owner_id = ?
               ORDER BY id`
	rows, err := r.db.QueryContext(ctx, q, cinemaID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Hall
	for rows.Next() {
		h := new(Hall)
		if err := rows.Scan(&h.ID, &h.OwnerID, &h.CinemaID, &h.Name, &h.Description, &h.SeatRows, &h.SeatCols, &h.IsActive, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateByIDAndOwner updates hall fields (name/description/seat_rows/seat_cols)
// if the hall belongs to the given owner.  Returns sql.ErrNoRows when not found.
func (r *HallRepo) UpdateByIDAndOwner(ctx context.Context, h *Hall) error {
	const q = `UPDATE halls
               SET name = ?, description = ?, seat_rows = ?, seat_cols = ?, updated_at = CURRENT_TIMESTAMP
               WHERE id = ? AND owner_id = ?`
	res, err := r.db.ExecContext(ctx, q,
		h.Name, h.Description, h.SeatRows, h.SeatCols, h.ID, h.OwnerID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
