package repository // repository for show seat persistence

import (
    "context"       // context for managing deadlines
    "database/sql"   // sql provides DB interfaces
    "strings"       // strings for building dynamic queries
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

// DB returns the underlying sql.DB used by the repository.  It allows
// callers outside the repository layer to begin their own transactions
// using the same database handle.  Use this with caution; ideally
// operations remain encapsulated within repository methods.
func (r *ShowSeatRepo) DB() *sql.DB { return r.db }

// SeatWithStatus represents a seat's position and its computed status in the
// context of a particular show.  It is returned by ListWithStatus and
// contains the row label, seat number, current status (FREE, HELD,
// RESERVED) and the price for the seat.  Clients can use this to
// construct a view of the auditorium with availability information.
type SeatWithStatus struct {
    SeatID     uint64 // seat_id
    RowLabel   string // seat row label
    SeatNumber uint32 // seat number within the row
    Status     string // computed status: FREE, HELD, RESERVED
    PriceCents uint32 // price in cents for this seat (from show_seats)
}

// ListWithStatus returns all seats for a show along with their availability
// status.  A seat is considered RESERVED when the show_seats.status is
// RESERVED.  It is considered HELD when there exists a non-expired
// entry in seat_holds for the same show and seat; otherwise it is
// considered FREE.  The computed status does not automatically clear
// expired holds; callers should ensure expired holds are purged or use
// this computed status to treat expired holds as FREE.
func (r *ShowSeatRepo) ListWithStatus(ctx context.Context, showID uint64) ([]SeatWithStatus, error) {
    const q = `SELECT s.id, s.row_label, s.seat_number, ss.status, ss.price_cents,
                      sh.id AS hold_id
               FROM seats s
               JOIN show_seats ss ON ss.seat_id = s.id AND ss.show_id = ?
               LEFT JOIN seat_holds sh ON sh.show_id = ss.show_id AND sh.seat_id = ss.seat_id AND sh.expires_at > UTC_TIMESTAMP()
               ORDER BY s.row_label, s.seat_number`
    rows, err := r.db.QueryContext(ctx, q, showID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var result []SeatWithStatus
    for rows.Next() {
        var id uint64
        var rowLabel string
        var seatNum uint32
        var seatStatus string
        var price uint32
        var holdID sql.NullInt64
        if err := rows.Scan(&id, &rowLabel, &seatNum, &seatStatus, &price, &holdID); err != nil {
            return nil, err
        }
        // compute final status: RESERVED has highest priority; then HELD (when hold exists);
        // otherwise FREE.
        status := "FREE"
        if seatStatus == "RESERVED" {
            status = "RESERVED"
        } else if holdID.Valid {
            status = "HELD"
        }
        result = append(result, SeatWithStatus{
            SeatID:     id,
            RowLabel:   rowLabel,
            SeatNumber: seatNum,
            Status:     status,
            PriceCents: price,
        })
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return result, nil
}

// FilterHoldableSeatsTx returns the subset of seatIDs that can be placed on hold
// for the specified show.  A seat is holdable when its show_seats.status is
// not RESERVED and there is no active seat_hold for it (expired holds do
// not block).  The query is executed within the provided transaction.
// The returned slice preserves the order of the input seatIDs.
func (r *ShowSeatRepo) FilterHoldableSeatsTx(ctx context.Context, tx *sql.Tx, showID uint64, seatIDs []uint64) ([]uint64, error) {
    if len(seatIDs) == 0 {
        return []uint64{}, nil
    }
    // Build IN clause placeholders
    placeholders := make([]string, 0, len(seatIDs))
    args := make([]interface{}, 0, len(seatIDs)+1)
    args = append(args, showID)
    for _, id := range seatIDs {
        placeholders = append(placeholders, "?")
        args = append(args, id)
    }
    // This query selects seat IDs that are holdable.  A seat is holdable if
    // it is not reserved and has no active hold.  We use a LEFT JOIN on
    // seat_holds with an expiration check to find active holds and exclude
    // them.  We also compare show_seats.status != 'RESERVED'.
    query := `SELECT ss.seat_id
              FROM show_seats ss
              LEFT JOIN seat_holds sh ON sh.show_id = ss.show_id AND sh.seat_id = ss.seat_id AND sh.expires_at > UTC_TIMESTAMP()
              WHERE ss.show_id = ? AND ss.seat_id IN (` + strings.Join(placeholders, ",") + `)
                AND ss.status <> 'RESERVED'
                AND sh.id IS NULL`
    rows, err := tx.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    // Build a set for constant time lookup
    allowed := make(map[uint64]struct{})
    for rows.Next() {
        var sid uint64
        if err := rows.Scan(&sid); err != nil {
            return nil, err
        }
        allowed[sid] = struct{}{}
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    // Preserve input order
    out := make([]uint64, 0, len(allowed))
    for _, sid := range seatIDs {
        if _, ok := allowed[sid]; ok {
            out = append(out, sid)
        }
    }
    return out, nil
}

// BulkUpdateStatusTx updates the status of the specified seats for a show.
// It sets show_seats.status to the provided status for each seat.  The
// update runs within the provided transaction.  Passing an empty
// seatID slice returns nil.  The version field is incremented by 1
// for optimistic locking semantics.  It does not check current
// status; callers must ensure the transition is valid.
func (r *ShowSeatRepo) BulkUpdateStatusTx(ctx context.Context, tx *sql.Tx, showID uint64, seatIDs []uint64, status string) error {
    if len(seatIDs) == 0 {
        return nil
    }
    placeholders := make([]string, 0, len(seatIDs))
    args := make([]interface{}, 0, len(seatIDs)+2)
    args = append(args, status, showID)
    for _, id := range seatIDs {
        placeholders = append(placeholders, "?")
        args = append(args, id)
    }
    query := `UPDATE show_seats
              SET status = ?, version = version + 1, updated_at = CURRENT_TIMESTAMP
              WHERE show_id = ? AND seat_id IN (` + strings.Join(placeholders, ",") + `)`
    _, err := tx.ExecContext(ctx, query, args...)
    return err
}

// GetPricesBySeatIDsTx returns a map of seat_id to price_cents for the
// specified seats within a show.  It is used when computing total
// amounts for reservations.  The caller must supply a transaction
// context.  All requested seat IDs must belong to the given show; if
// they do not, the map will not contain those keys.  Passing an empty
// slice results in an empty map.
func (r *ShowSeatRepo) GetPricesBySeatIDsTx(ctx context.Context, tx *sql.Tx, showID uint64, seatIDs []uint64) (map[uint64]uint32, error) {
    result := make(map[uint64]uint32)
    if len(seatIDs) == 0 {
        return result, nil
    }
    placeholders := make([]string, 0, len(seatIDs))
    args := make([]interface{}, 0, len(seatIDs)+1)
    args = append(args, showID)
    for _, id := range seatIDs {
        placeholders = append(placeholders, "?")
        args = append(args, id)
    }
    query := `SELECT seat_id, price_cents
              FROM show_seats
              WHERE show_id = ? AND seat_id IN (` + strings.Join(placeholders, ",") + `)`
    rows, err := tx.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    for rows.Next() {
        var sid uint64
        var price uint32
        if err := rows.Scan(&sid, &price); err != nil {
            return nil, err
        }
        result[sid] = price
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return result, nil
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

// CreateBulkTx inserts multiple show_seat records within the scope of an existing
// transaction.  This method mirrors CreateBulk but uses the provided *sql.Tx
// instead of the repository's DB handle, allowing callers to compose
// show_seat inserts with other operations in a single atomic transaction.
// The caller is responsible for committing or rolling back the transaction.
func (r *ShowSeatRepo) CreateBulkTx(ctx context.Context, tx *sql.Tx, seats []ShowSeat) error {
    if len(seats) == 0 {
        return nil
    }
    // Build the insert statement with one set of placeholders per seat.
    query := `INSERT INTO show_seats (show_id, seat_id, status, price_cents, version) VALUES `
    args := make([]interface{}, 0, len(seats)*5)
    for i, ss := range seats {
        if i > 0 {
            query += ","
        }
        query += "(?, ?, ?, ?, ?)"
        args = append(args, ss.ShowID, ss.SeatID, ss.Status, ss.PriceCents, ss.Version)
    }
    // Execute the bulk insert within the provided transaction context.
    _, err := tx.ExecContext(ctx, query, args...)
    return err
}