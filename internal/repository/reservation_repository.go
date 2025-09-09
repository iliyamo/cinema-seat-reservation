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

// OwnerReservationDetail extends the information returned for a reservation when
// viewed by a hall owner.  In addition to the fields available in
// ReservationDetail, it includes the ID of the user who created the
// reservation and the optional payment reference.  This type is used by
// owner‑specific endpoints to expose the reservation's customer and payment
// details alongside show, hall, cinema and seat information.
type OwnerReservationDetail struct {
    ID               uint64   `json:"id"`
    UserID           uint64   `json:"user_id"`
    ShowID           uint64   `json:"show_id"`
    Status           string   `json:"status"`
    TotalAmountCents uint32   `json:"total_amount_cents"`
    PaymentRef       *string  `json:"payment_ref,omitempty"`
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

// GetByIDForUser returns a single reservation for the given user.  It
// loads the reservation's show, hall and cinema details and populates
// the list of seats booked under the reservation.  When no reservation
// with the specified ID exists for the user, sql.ErrNoRows is returned.
func (r *ReservationRepo) GetByIDForUser(ctx context.Context, reservationID, userID uint64) (*ReservationDetail, error) {
    // Query reservation and related show/hall/cinema information.  Restrict
    // to the requested reservation ID and the calling user to enforce
    // ownership.
    const q = `SELECT r.id, r.show_id, r.status, r.total_amount_cents,
                      s.title, s.starts_at, s.ends_at,
                      h.id, h.name, c.id, c.name
               FROM reservations r
               JOIN shows s ON s.id = r.show_id
               JOIN halls h ON h.id = s.hall_id
               LEFT JOIN cinemas c ON c.id = h.cinema_id
               WHERE r.id = ? AND r.user_id = ?`
    var det ReservationDetail
    var hallID uint64
    var hallName string
    var cinemaID sql.NullInt64
    var cinemaName sql.NullString
    var startStr, endStr sql.NullString
    // Execute the query; if no row is returned the error is sql.ErrNoRows
    err := r.db.QueryRowContext(ctx, q, reservationID, userID).Scan(
        &det.ID, &det.ShowID, &det.Status, &det.TotalAmountCents,
        &det.ShowTitle, &startStr, &endStr,
        &hallID, &hallName, &cinemaID, &cinemaName,
    )
    if err != nil {
        return nil, err
    }
    // Convert DB timestamps (YYYY‑MM‑DD HH:MM:SS) to RFC3339 in UTC
    if startStr.Valid && strings.TrimSpace(startStr.String) != "" && startStr.String != "0001-01-01 00:00:00" {
        if t, err2 := time.Parse("2006-01-02 15:04:05", startStr.String); err2 == nil {
            iso := t.UTC().Format(time.RFC3339)
            det.StartTime = &iso
        }
    }
    if endStr.Valid && strings.TrimSpace(endStr.String) != "" && endStr.String != "0001-01-01 00:00:00" {
        if t, err2 := time.Parse("2006-01-02 15:04:05", endStr.String); err2 == nil {
            iso := t.UTC().Format(time.RFC3339)
            det.EndTime = &iso
        }
    }
    det.HallID = hallID
    det.HallName = hallName
    if cinemaID.Valid {
        cid := uint64(cinemaID.Int64)
        det.CinemaID = &cid
    }
    if cinemaName.Valid {
        cn := cinemaName.String
        det.CinemaName = &cn
    }
    det.Seats = []struct {
        SeatID     uint64 `json:"seat_id"`
        RowLabel   string `json:"row_label"`
        SeatNumber uint32 `json:"seat_number"`
    }{}
    // Query all seats for this reservation.  Ordering by row and seat number
    // provides deterministic output.
    const seatQ = `SELECT rs.seat_id, se.row_label, se.seat_number
                   FROM reservation_seats rs
                   JOIN seats se ON se.id = rs.seat_id
                   WHERE rs.reservation_id = ?
                   ORDER BY se.row_label, se.seat_number`
    srows, err := r.db.QueryContext(ctx, seatQ, det.ID)
    if err != nil {
        return nil, err
    }
    defer srows.Close()
    for srows.Next() {
        var sid uint64
        var rowLabel string
        var seatNum uint32
        if err := srows.Scan(&sid, &rowLabel, &seatNum); err != nil {
            return nil, err
        }
        det.Seats = append(det.Seats, struct {
            SeatID     uint64 `json:"seat_id"`
            RowLabel   string `json:"row_label"`
            SeatNumber uint32 `json:"seat_number"`
        }{SeatID: sid, RowLabel: rowLabel, SeatNumber: seatNum})
    }
    if err := srows.Err(); err != nil {
        return nil, err
    }
    return &det, nil
}

// GetByIDForOwner returns a reservation and its details when accessed
// by a hall owner.  It verifies that the reservation exists and that
// the owner owns the hall associated with the reservation's show.  On
// success it returns an OwnerReservationDetail populated with show,
// hall, cinema and seat information as well as the user ID and
// payment reference.  It returns ErrForbidden when the owner does
// not own the underlying hall and sql.ErrNoRows when the reservation
// does not exist.
func (r *ReservationRepo) GetByIDForOwner(ctx context.Context, reservationID, ownerID uint64) (*OwnerReservationDetail, error) {
    // First check existence and ownership.  Join through shows and halls to
    // obtain the owner_id.  If no row is returned, the reservation does
    // not exist.  If the owner_id does not match, return ErrForbidden.
    const checkQ = `SELECT h.owner_id
                    FROM reservations r
                    JOIN shows s ON s.id = r.show_id
                    JOIN halls h ON h.id = s.hall_id
                    WHERE r.id = ?`
    var actualOwnerID uint64
    err := r.db.QueryRowContext(ctx, checkQ, reservationID).Scan(&actualOwnerID)
    if err != nil {
        return nil, err
    }
    if actualOwnerID != ownerID {
        return nil, ErrForbidden
    }
    // Fetch the reservation details including the user ID and payment ref
    const q = `SELECT r.id, r.user_id, r.show_id, r.status, r.total_amount_cents, r.payment_ref,
                      s.title, s.starts_at, s.ends_at,
                      h.id, h.name, c.id, c.name
               FROM reservations r
               JOIN shows s ON s.id = r.show_id
               JOIN halls h ON h.id = s.hall_id
               LEFT JOIN cinemas c ON c.id = h.cinema_id
               WHERE r.id = ?`
    var det OwnerReservationDetail
    var payRef sql.NullString
    var hallID uint64
    var hallName string
    var cinemaID sql.NullInt64
    var cinemaName sql.NullString
    var startStr, endStr sql.NullString
    if err := r.db.QueryRowContext(ctx, q, reservationID).Scan(
        &det.ID, &det.UserID, &det.ShowID, &det.Status, &det.TotalAmountCents, &payRef,
        &det.ShowTitle, &startStr, &endStr,
        &hallID, &hallName, &cinemaID, &cinemaName,
    ); err != nil {
        return nil, err
    }
    if payRef.Valid {
        ref := payRef.String
        det.PaymentRef = &ref
    }
    // Convert times
    if startStr.Valid && strings.TrimSpace(startStr.String) != "" && startStr.String != "0001-01-01 00:00:00" {
        if t, err2 := time.Parse("2006-01-02 15:04:05", startStr.String); err2 == nil {
            iso := t.UTC().Format(time.RFC3339)
            det.StartTime = &iso
        }
    }
    if endStr.Valid && strings.TrimSpace(endStr.String) != "" && endStr.String != "0001-01-01 00:00:00" {
        if t, err2 := time.Parse("2006-01-02 15:04:05", endStr.String); err2 == nil {
            iso := t.UTC().Format(time.RFC3339)
            det.EndTime = &iso
        }
    }
    det.HallID = hallID
    det.HallName = hallName
    if cinemaID.Valid {
        cid := uint64(cinemaID.Int64)
        det.CinemaID = &cid
    }
    if cinemaName.Valid {
        cn := cinemaName.String
        det.CinemaName = &cn
    }
    det.Seats = []struct {
        SeatID     uint64 `json:"seat_id"`
        RowLabel   string `json:"row_label"`
        SeatNumber uint32 `json:"seat_number"`
    }{}
    // Fetch seats booked under this reservation
    const seatQ = `SELECT rs.seat_id, se.row_label, se.seat_number
                   FROM reservation_seats rs
                   JOIN seats se ON se.id = rs.seat_id
                   WHERE rs.reservation_id = ?
                   ORDER BY se.row_label, se.seat_number`
    rows, err := r.db.QueryContext(ctx, seatQ, det.ID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    for rows.Next() {
        var sid uint64
        var rowLabel string
        var seatNum uint32
        if err := rows.Scan(&sid, &rowLabel, &seatNum); err != nil {
            return nil, err
        }
        det.Seats = append(det.Seats, struct {
            SeatID     uint64 `json:"seat_id"`
            RowLabel   string `json:"row_label"`
            SeatNumber uint32 `json:"seat_number"`
        }{SeatID: sid, RowLabel: rowLabel, SeatNumber: seatNum})
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return &det, nil
}

// ListByShowForOwner returns all reservations for a given show when
// accessed by its hall owner.  It verifies that the show belongs to
// the owner before returning the list; otherwise ErrForbidden is
// returned.  Reservations are ordered by creation time descending.
func (r *ReservationRepo) ListByShowForOwner(ctx context.Context, showID, ownerID uint64) ([]OwnerReservationDetail, error) {
    // Verify that the show is owned by the caller.  Join through halls to
    // obtain the owner_id.  If no row is returned then the show does
    // not exist (sql.ErrNoRows).  If the owner does not match, return
    // ErrForbidden.
    const checkQ = `SELECT h.owner_id
                    FROM shows s
                    JOIN halls h ON h.id = s.hall_id
                    WHERE s.id = ?`
    var actualOwnerID uint64
    err := r.db.QueryRowContext(ctx, checkQ, showID).Scan(&actualOwnerID)
    if err != nil {
        return nil, err
    }
    if actualOwnerID != ownerID {
        return nil, ErrForbidden
    }
    // Fetch reservations for the show with user and payment info
    const q = `SELECT r.id, r.user_id, r.show_id, r.status, r.total_amount_cents, r.payment_ref,
                      s.title, s.starts_at, s.ends_at,
                      h.id, h.name, c.id, c.name,
                      r.created_at
               FROM reservations r
               JOIN shows s ON s.id = r.show_id
               JOIN halls h ON h.id = s.hall_id
               LEFT JOIN cinemas c ON c.id = h.cinema_id
               WHERE r.show_id = ?
               ORDER BY r.created_at DESC`
    rows, err := r.db.QueryContext(ctx, q, showID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    details := make([]OwnerReservationDetail, 0)
    index := make(map[uint64]int)
    for rows.Next() {
        var d OwnerReservationDetail
        var payRef sql.NullString
        var hallID uint64
        var hallName string
        var cinemaID sql.NullInt64
        var cinemaName sql.NullString
        var startStr, endStr sql.NullString
        var createdAt time.Time
        if err := rows.Scan(
            &d.ID, &d.UserID, &d.ShowID, &d.Status, &d.TotalAmountCents, &payRef,
            &d.ShowTitle, &startStr, &endStr,
            &hallID, &hallName, &cinemaID, &cinemaName,
            &createdAt,
        ); err != nil {
            return nil, err
        }
        if payRef.Valid {
            ref := payRef.String
            d.PaymentRef = &ref
        }
        // parse start and end times to RFC3339
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
    // Populate seats for all reservations in a single query
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
        var rid uint64
        var sid uint64
        var rowLabel string
        var seatNum uint32
        if err := srows.Scan(&rid, &sid, &rowLabel, &seatNum); err != nil {
            return nil, err
        }
        idx, ok := index[rid]
        if !ok {
            continue
        }
        details[idx].Seats = append(details[idx].Seats, struct {
            SeatID     uint64 `json:"seat_id"`
            RowLabel   string `json:"row_label"`
            SeatNumber uint32 `json:"seat_number"`
        }{SeatID: sid, RowLabel: rowLabel, SeatNumber: seatNum})
    }
    if err := srows.Err(); err != nil {
        return nil, err
    }
    return details, nil
}

// GetInfoForOwnerTx returns the show ID, show start time and list of seat IDs for a
// reservation, validating ownership within a transaction.  It ensures
// that the reservation exists and that the caller owns the hall.  If
// the reservation does not exist, sql.ErrNoRows is returned.  If the
// owner does not own the hall, ErrForbidden is returned.  The
// returned time is in UTC.
func (r *ReservationRepo) GetInfoForOwnerTx(ctx context.Context, tx *sql.Tx, reservationID, ownerID uint64) (uint64, time.Time, []uint64, error) {
    const q = `SELECT r.show_id, s.starts_at, h.owner_id
               FROM reservations r
               JOIN shows s ON s.id = r.show_id
               JOIN halls h ON h.id = s.hall_id
               WHERE r.id = ?`
    var showID uint64
    var startStr string
    var actualOwnerID uint64
    err := tx.QueryRowContext(ctx, q, reservationID).Scan(&showID, &startStr, &actualOwnerID)
    if err != nil {
        return 0, time.Time{}, nil, err
    }
    if actualOwnerID != ownerID {
        return 0, time.Time{}, nil, ErrForbidden
    }
    // Parse start time
    t, err := time.Parse("2006-01-02 15:04:05", startStr)
    if err != nil {
        return 0, time.Time{}, nil, err
    }
    // Fetch seat IDs
    const seatQ = `SELECT seat_id FROM reservation_seats WHERE reservation_id = ?`
    rows, err := tx.QueryContext(ctx, seatQ, reservationID)
    if err != nil {
        return 0, time.Time{}, nil, err
    }
    defer rows.Close()
    var seatIDs []uint64
    for rows.Next() {
        var sid uint64
        if err := rows.Scan(&sid); err != nil {
            return 0, time.Time{}, nil, err
        }
        seatIDs = append(seatIDs, sid)
    }
    if err := rows.Err(); err != nil {
        return 0, time.Time{}, nil, err
    }
    return showID, t.UTC(), seatIDs, nil
}

// GetInfoForUserTx returns the show ID, show start time and seat IDs for a
// reservation within a transaction, validating that the reservation
// belongs to the specified user.  It returns sql.ErrNoRows when the
// reservation does not exist and ErrForbidden when the reservation
// belongs to a different user.  The returned time is in UTC.
func (r *ReservationRepo) GetInfoForUserTx(ctx context.Context, tx *sql.Tx, reservationID, userID uint64) (uint64, time.Time, []uint64, error) {
    const q = `SELECT r.show_id, s.starts_at, r.user_id
               FROM reservations r
               JOIN shows s ON s.id = r.show_id
               WHERE r.id = ?`
    var showID uint64
    var startStr string
    var actualUserID uint64
    err := tx.QueryRowContext(ctx, q, reservationID).Scan(&showID, &startStr, &actualUserID)
    if err != nil {
        return 0, time.Time{}, nil, err
    }
    if actualUserID != userID {
        return 0, time.Time{}, nil, ErrForbidden
    }
    t, err := time.Parse("2006-01-02 15:04:05", startStr)
    if err != nil {
        return 0, time.Time{}, nil, err
    }
    const seatQ = `SELECT seat_id FROM reservation_seats WHERE reservation_id = ?`
    rows, err := tx.QueryContext(ctx, seatQ, reservationID)
    if err != nil {
        return 0, time.Time{}, nil, err
    }
    defer rows.Close()
    var seatIDs []uint64
    for rows.Next() {
        var sid uint64
        if err := rows.Scan(&sid); err != nil {
            return 0, time.Time{}, nil, err
        }
        seatIDs = append(seatIDs, sid)
    }
    if err := rows.Err(); err != nil {
        return 0, time.Time{}, nil, err
    }
    return showID, t.UTC(), seatIDs, nil
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