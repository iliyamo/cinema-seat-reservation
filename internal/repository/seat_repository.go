package repository // repository defines data access for seats

import (
	"context"      // context allows query cancellation and timeouts
	"database/sql" // sql provides DB primitives
	"errors"       // errors for sentinel definitions
)

// Seat represents a physical seat within a hall. RowLabel and
// SeatNumber identify the seat's position; SeatType indicates its class.
type Seat struct {
	ID         uint64 // primary key
	HallID     uint64 // FK -> halls.id
	RowLabel   string // e.g. A, B, AA
	SeatNumber uint32 // position in the row (1-based)
	SeatType   string // STANDARD | VIP | ACCESSIBLE
	IsActive   bool   // soft availability flag (not reservation)
	CreatedAt  string
	UpdatedAt  string
}

// ErrSeatNotFound is returned when a seat lookup yields no rows.
var ErrSeatNotFound = errors.New("seat not found")

// SeatRepo provides methods to work with seats in the database.
type SeatRepo struct {
	db *sql.DB
}

// NewSeatRepo constructs a SeatRepo with the given DB handle.
func NewSeatRepo(db *sql.DB) *SeatRepo {
	return &SeatRepo{db: db}
}

// Create inserts a single seat record. On success the seat's ID is populated.
func (r *SeatRepo) Create(ctx context.Context, s *Seat) error {
	const q = `INSERT INTO seats (hall_id, row_label, seat_number, seat_type)
	           VALUES (?, ?, ?, ?)`
	res, err := r.db.ExecContext(ctx, q, s.HallID, s.RowLabel, s.SeatNumber, s.SeatType)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	s.ID = uint64(id)
	return nil
}

// CreateBulk inserts multiple seats in a single statement.
func (r *SeatRepo) CreateBulk(ctx context.Context, seats []Seat) error {
	if len(seats) == 0 {
		return nil
	}
	query := `INSERT INTO seats (hall_id, row_label, seat_number, seat_type) VALUES `
	args := make([]interface{}, 0, len(seats)*4)
	for i, seat := range seats {
		if i > 0 {
			query += ","
		}
		query += "(?, ?, ?, ?)"
		args = append(args, seat.HallID, seat.RowLabel, seat.SeatNumber, seat.SeatType)
	}
	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

// GetByHall retrieves all seats of a hall ordered by row_label then seat_number.
func (r *SeatRepo) GetByHall(ctx context.Context, hallID uint64) ([]Seat, error) {
	const q = `SELECT id, hall_id, row_label, seat_number, seat_type, is_active, created_at, updated_at
	           FROM seats
	           WHERE hall_id = ?
	           ORDER BY row_label, seat_number`
	rows, err := r.db.QueryContext(ctx, q, hallID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Seat
	for rows.Next() {
		var s Seat
		if err := rows.Scan(
			&s.ID, &s.HallID, &s.RowLabel, &s.SeatNumber, &s.SeatType,
			&s.IsActive, &s.CreatedAt, &s.UpdatedAt,
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

// GetByID retrieves a seat by its id (no ownership check).
func (r *SeatRepo) GetByID(ctx context.Context, id uint64) (*Seat, error) {
	const q = `SELECT id, hall_id, row_label, seat_number, seat_type, is_active, created_at, updated_at
	           FROM seats WHERE id = ?`
	var s Seat
	err := r.db.QueryRowContext(ctx, q, id).
		Scan(&s.ID, &s.HallID, &s.RowLabel, &s.SeatNumber, &s.SeatType, &s.IsActive, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSeatNotFound
		}
		return nil, err
	}
	return &s, nil
}

// GetByIDAndOwner retrieves a seat by its id while enforcing ownership via halls.
func (r *SeatRepo) GetByIDAndOwner(ctx context.Context, id, ownerID uint64) (*Seat, error) {
	const q = `SELECT s.id, s.hall_id, s.row_label, s.seat_number, s.seat_type, s.is_active, s.created_at, s.updated_at
	           FROM seats s
	           JOIN halls h ON h.id = s.hall_id
	           WHERE s.id = ? AND h.owner_id = ?`
	var s Seat
	err := r.db.QueryRowContext(ctx, q, id, ownerID).
		Scan(&s.ID, &s.HallID, &s.RowLabel, &s.SeatNumber, &s.SeatType, &s.IsActive, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSeatNotFound
		}
		return nil, err
	}
	return &s, nil
}

// UpdateByIDAndOwner updates row_label, seat_number, is_active (without seat_type).
// Returns sql.ErrNoRows when not found or not owned by this owner.
func (r *SeatRepo) UpdateByIDAndOwner(ctx context.Context, id, ownerID uint64, rowLabel string, seatNumber uint32, isActive bool) error {
	const q = `UPDATE seats s
	           JOIN halls h ON h.id = s.hall_id
	           SET s.row_label = ?, s.seat_number = ?, s.is_active = ?, s.updated_at = CURRENT_TIMESTAMP
	           WHERE s.id = ? AND h.owner_id = ?`
	res, err := r.db.ExecContext(ctx, q, rowLabel, seatNumber, isActive, id, ownerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateWithTypeByIDAndOwner updates row_label, seat_number, seat_type, is_active.
// Use this when you also want to change the seat type.
func (r *SeatRepo) UpdateWithTypeByIDAndOwner(ctx context.Context, id, ownerID uint64, rowLabel string, seatNumber uint32, seatType string, isActive bool) error {
	const q = `UPDATE seats s
	           JOIN halls h ON h.id = s.hall_id
	           SET s.row_label = ?, s.seat_number = ?, s.seat_type = ?, s.is_active = ?, s.updated_at = CURRENT_TIMESTAMP
	           WHERE s.id = ? AND h.owner_id = ?`
	res, err := r.db.ExecContext(ctx, q, rowLabel, seatNumber, seatType, isActive, id, ownerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateFullByIDAndOwner is an alias kept for handler compatibility.
func (r *SeatRepo) UpdateFullByIDAndOwner(ctx context.Context, id, ownerID uint64, rowLabel string, seatNumber uint32, seatType string, isActive bool) error {
	return r.UpdateWithTypeByIDAndOwner(ctx, id, ownerID, rowLabel, seatNumber, seatType, isActive)
}

// DeleteByIDAndOwner deletes a seat while ensuring the hall belongs to the owner.
func (r *SeatRepo) DeleteByIDAndOwner(ctx context.Context, id, ownerID uint64) error {
	const q = `DELETE s FROM seats s
	           JOIN halls h ON h.id = s.hall_id
	           WHERE s.id = ? AND h.owner_id = ?`
	res, err := r.db.ExecContext(ctx, q, id, ownerID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteByHall removes all seats associated with a given hall ID.  This is used
// when a hall's layout (rows/columns) is changed and all seats must be
// regenerated.  It does not perform any ownership checks – callers should
// verify the hall belongs to the current owner prior to calling this method.
func (r *SeatRepo) DeleteByHall(ctx context.Context, hallID uint64) error {
    // Construct a simple delete statement to remove all seat rows for the hall.
    const q = `DELETE FROM seats WHERE hall_id = ?` // query deleting seats by hall
    _, err := r.db.ExecContext(ctx, q, hallID)      // execute deletion
    return err                                      // return any error encountered
}
