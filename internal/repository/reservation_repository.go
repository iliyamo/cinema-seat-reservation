package repository

import (
    "context"
    "database/sql"
    "strings"
    "time"
)

// ReservationRepo provides CRUD operations for reservations and their seats.
// Reservations group together one or more seats for a particular show and
// user.  Seats reserved under a reservation are stored in the
// reservation_seats table.  All timestamp fields are assumed to be
// stored in UTC.
type ReservationRepo struct {
    db *sql.DB
}

// NewReservationRepo returns a new ReservationRepo bound to the given database.
func NewReservationRepo(db *sql.DB) *ReservationRepo { return &ReservationRepo{db: db} }

// ReservationRecord mirrors the schema of the reservations table.  It is
// used internally by the repository when constructing or scanning rows.
// Business logic should use the model.Reservation type instead.
type ReservationRecord struct {
    ID               uint64
    UserID           uint64
    ShowID           uint64
    Status           string
    TotalAmountCents uint32
    PaymentRef       *string
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

// ReservationSeatRecord mirrors the reservation_seats table.  It maps a
// reservation to a specific seat and price.  Only fields needed for
// insertion are exposed.
type ReservationSeatRecord struct {
    ReservationID uint64
    ShowID        uint64
    SeatID        uint64
    PriceCents    uint32
}

// CreateTx inserts a new reservation within the scope of an existing
// transaction.  It populates the generated ID on the provided record and
// returns any error from the database.  The caller must commit or
// rollback the transaction.  Status should be a valid enumeration
// ('PENDING','CONFIRMED','CANCELLED').
func (r *ReservationRepo) CreateTx(ctx context.Context, tx *sql.Tx, res *ReservationRecord) error {
    const q = `INSERT INTO reservations (user_id, show_id, status, total_amount_cents) VALUES (?, ?, ?, ?)`
    result, err := tx.ExecContext(ctx, q, res.UserID, res.ShowID, res.Status, res.TotalAmountCents)
    if err != nil {
        return err
    }
    id, err := result.LastInsertId()
    if err != nil {
        return err
    }
    res.ID = uint64(id)
    // Query back the full row to populate timestamps and defaults
    const sel = `SELECT id, user_id, show_id, status, total_amount_cents, payment_ref, created_at, updated_at FROM reservations WHERE id = ?`
    var paymentRef sql.NullString
    err = tx.QueryRowContext(ctx, sel, res.ID).Scan(
        &res.ID, &res.UserID, &res.ShowID, &res.Status, &res.TotalAmountCents,
        &paymentRef, &res.CreatedAt, &res.UpdatedAt,
    )
    if err != nil {
        return err
    }
    if paymentRef.Valid {
        pr := paymentRef.String
        res.PaymentRef = &pr
    }
    return nil
}

// CreateSeatsBulkTx inserts multiple reservation_seats rows in a single
// statement.  It associates each seat with the same reservation.  The
// caller must supply the reservation ID in each record.  The insertion
// occurs within the provided transaction.  Passing an empty slice has
// no effect and returns nil.
func (r *ReservationRepo) CreateSeatsBulkTx(ctx context.Context, tx *sql.Tx, seats []ReservationSeatRecord) error {
    if len(seats) == 0 {
        return nil
    }
    query := `INSERT INTO reservation_seats (reservation_id, show_id, seat_id, price_cents) VALUES `
    args := make([]interface{}, 0, len(seats)*4)
    for i, s := range seats {
        if i > 0 {
            query += ","
        }
        query += "(?, ?, ?, ?)"
        args = append(args, s.ReservationID, s.ShowID, s.SeatID, s.PriceCents)
    }
    _, err := tx.ExecContext(ctx, query, args...)
    return err
}

// ReservationDetail encapsulates a reservation along with related show,
// hall and cinema information and the seats reserved.  It is returned by
// ListByUser for display to customers.
type ReservationDetail struct {
    ID               uint64   `json:"id"`
    ShowID           uint64   `json:"show_id"`
    Status           string   `json:"status"`
    TotalAmountCents uint32   `json:"total_amount_cents"`
    ShowTitle        string   `json:"show_title"`
    StartTime        *string  `json:"start_time"`
    EndTime          *string  `json:"end_time"`
    HallID           uint64   `json:"hall_id"`
    HallName         string   `json:"hall_name"`
    CinemaID         *uint64  `json:"cinema_id,omitempty"`
    CinemaName       *string  `json:"cinema_name,omitempty"`
    Seats            []struct {
        SeatID     uint64 `json:"seat_id"`
        RowLabel   string `json:"row_label"`
        SeatNumber uint32 `json:"seat_number"`
    } `json:"seats"`
}

// ListByUser returns all reservations for the given user along with show,
// hall, cinema and seat details.  It assembles the results into
// ReservationDetail structs.  Reservations are ordered by creation time
// descending (newest first).  When no reservations exist, an empty
// slice is returned.
func (r *ReservationRepo) ListByUser(ctx context.Context, userID uint64) ([]ReservationDetail, error) {
    // First fetch high-level reservation info and related show/hall/cinema details
    const q = `SELECT r.id, r.show_id, r.status, r.total_amount_cents,
                      s.title, s.starts_at, s.ends_at,
                      h.id, h.name, c.id, c.name,
                      r.created_at
               FROM reservations r
               JOIN shows s ON s.id = r.show_id
               JOIN halls h ON h.id = s.hall_id
               LEFT JOIN cinemas c ON c.id = h.cinema_id
               WHERE r.user_id = ?
               ORDER BY r.created_at DESC`
    rows, err := r.db.QueryContext(ctx, q, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    // We'll build a map from reservation ID to its detail to allow seat population.
    details := make([]ReservationDetail, 0)
    // Keep track of index by reservation ID for quick lookup
    index := make(map[uint64]int)
    for rows.Next() {
        var d ReservationDetail
        var hallID uint64
        var hallName string
        var cinemaID sql.NullInt64
        var cinemaName sql.NullString
        var startStr, endStr sql.NullString
        var createdAt time.Time
        if err := rows.Scan(
            &d.ID, &d.ShowID, &d.Status, &d.TotalAmountCents,
            &d.ShowTitle, &startStr, &endStr,
            &hallID, &hallName, &cinemaID, &cinemaName,
            &createdAt,
        ); err != nil {
            return nil, err
        }
        // Convert times from DB format to ISO8601
        if startStr.Valid && strings.TrimSpace(startStr.String) != "" && startStr.String != "0001-01-01 00:00:00" {
            if t, err2 := time.Parse("2006-01-02 15:04:05", startStr.String); err2 == nil {
                iso := t.UTC().Format(time.RFC3339)
                d.StartTime = &iso
            }
        }
        if endStr.Valid && strings.TrimSpace(endStr.String) != "" && endStr.String != "0001-01-01 00:00:00" {
            if t, err2 := time.Parse("2006-01-02 15:04:05", endStr.String); err2 == nil {
                iso := t.UTC().Format(time.RFC3339)
                d.EndTime = &iso
            }
        }
        d.HallID = hallID
        d.HallName = hallName
        if cinemaID.Valid {
            cid := uint64(cinemaID.Int64)
            d.CinemaID = &cid
        }
        if cinemaName.Valid {
            cn := cinemaName.String
            d.CinemaName = &cn
        }
        d.Seats = []struct {
            SeatID     uint64 `json:"seat_id"`
            RowLabel   string `json:"row_label"`
            SeatNumber uint32 `json:"seat_number"`
        }{}
        index[d.ID] = len(details)
        details = append(details, d)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    if len(details) == 0 {
        return details, nil
    }
    // Fetch seats for all reservations in one query
    // Build placeholders for reservation IDs
    ids := make([]interface{}, 0, len(details))
    placeholders := make([]string, 0, len(details))
    for _, d := range details {
        ids = append(ids, d.ID)
        placeholders = append(placeholders, "?")
    }
    seatQuery := `SELECT rs.reservation_id, rs.seat_id, se.row_label, se.seat_number
                  FROM reservation_seats rs
                  JOIN seats se ON se.id = rs.seat_id
                  WHERE rs.reservation_id IN (` + strings.Join(placeholders, ",") + `)
                  ORDER BY rs.reservation_id, se.row_label, se.seat_number`
    srows, err := r.db.QueryContext(ctx, seatQuery, ids...)
    if err != nil {
        return nil, err
    }
    defer srows.Close()
    for srows.Next() {
        var resID uint64
        var sid uint64
        var rowLabel string
        var seatNumber uint32
        if err := srows.Scan(&resID, &sid, &rowLabel, &seatNumber); err != nil {
            return nil, err
        }
        // append to corresponding reservation
        idx, ok := index[resID]
        if !ok {
            continue
        }
        details[idx].Seats = append(details[idx].Seats, struct {
            SeatID     uint64 `json:"seat_id"`
            RowLabel   string `json:"row_label"`
            SeatNumber uint32 `json:"seat_number"`
        }{SeatID: sid, RowLabel: rowLabel, SeatNumber: seatNumber})
    }
    if err := srows.Err(); err != nil {
        return nil, err
    }
    return details, nil
}