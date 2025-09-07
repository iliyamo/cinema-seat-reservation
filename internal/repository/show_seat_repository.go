package repository // repository for show seat persistence

import (
    "context"       // context for managing deadlines
    "database/sql"   // sql provides DB interfaces
)

// ShowSeat represents the availability and pricing of a specific seat
// for a particular show.  Each combination of show and seat is unique.
type ShowSeat struct {
    ID         uint64 // ID is the primary key of the show_seat row
    ShowID     uint64 // ShowID references the show
    SeatID     uint64 // SeatID references the seat
    Status     string // Status is one of FREE, HELD, RESERVED
    PriceCents uint32 // PriceCents is the price for this seat
    Version    uint32 // Version is used for optimistic locking (not enforced here)
    CreatedAt  string // CreatedAt records when the row was inserted
    UpdatedAt  string // UpdatedAt records last modification
}

// ShowSeatRepo encapsulates database operations for show_seats.
type ShowSeatRepo struct {
    db *sql.DB
}

// NewShowSeatRepo constructs a ShowSeatRepo given a DB handle.
func NewShowSeatRepo(db *sql.DB) *ShowSeatRepo {
    return &ShowSeatRepo{db: db}
}

// CreateBulk inserts multiple show_seat records in one statement.  It
// accepts a slice of ShowSeat values.  Only show_id, seat_id,
// status, price_cents and version are inserted.  CreatedAt/UpdatedAt
// timestamps default in the DB.  The ID fields of the passed
// structures are not populated.
func (r *ShowSeatRepo) CreateBulk(ctx context.Context, seats []ShowSeat) error {
    if len(seats) == 0 {
        return nil
    }
    // Build the INSERT with placeholders for each seat.  Each row
    // requires five values.  We rely on the DB defaults for timestamps.
    query := `INSERT INTO show_seats (show_id, seat_id, status, price_cents, version) VALUES `
    args := make([]interface{}, 0, len(seats)*5)
    for i, ss := range seats {
        if i > 0 {
            query += ","
        }
        query += "(?, ?, ?, ?, ?)"
        args = append(args, ss.ShowID, ss.SeatID, ss.Status, ss.PriceCents, ss.Version)
    }
    _, err := r.db.ExecContext(ctx, query, args...)
    return err
}