package repository

import (
    "context"
    "crypto/rand"
    "database/sql"
    "encoding/hex"
    "errors"
    "time"
)

// SeatHoldRecord represents the persistence model for a seat hold.  It is
// used internally by the repository layer when creating and querying holds.
// The exported model.SeatHold should be used for business logic.
type SeatHoldRecord struct {
    ID        uint64    // primary key of the seat_holds row
    UserID    uint64    // user who holds the seat; must be non-zero for authenticated holds
    ShowID    uint64    // show to which this seat belongs
    SeatID    uint64    // seat being held
    HoldToken string    // opaque token returned to the client for correlation
    ExpiresAt time.Time // expiration timestamp
    CreatedAt time.Time // creation timestamp
}

// SeatHoldRepo provides data access to the seat_holds table.  It is
// responsible for creating, listing and deleting seat holds.  All methods
// behave with respect to UTC timestamps – callers must ensure that
// expiration comparisons are performed in UTC.
type SeatHoldRepo struct {
    db *sql.DB
}

// NewSeatHoldRepo returns a new SeatHoldRepo bound to the provided database.
func NewSeatHoldRepo(db *sql.DB) *SeatHoldRepo { return &SeatHoldRepo{db: db} }

// ExpireHoldsTx removes all seat holds for a given show that have expired and
// returns the seat IDs whose holds were removed.  A hold is considered
// expired when its expires_at timestamp is less than or equal to the current
// UTC time.  The caller must supply an existing transaction and is
// responsible for committing or rolling back the transaction.  After
// calling ExpireHoldsTx, callers should update the corresponding
// show_seats.status values back to "FREE" for the returned seat IDs.
//
// When there are no expired holds, it returns an empty slice and nil error.
func (r *SeatHoldRepo) ExpireHoldsTx(ctx context.Context, tx *sql.Tx, showID uint64) ([]uint64, error) {
    // Query all seat IDs with expired holds for this show.
    rows, err := tx.QueryContext(ctx,
        `SELECT seat_id FROM seat_holds WHERE show_id = ? AND expires_at <= UTC_TIMESTAMP()`,
        showID,
    )
    if err != nil {
        return nil, err
    }
    var expiredSeatIDs []uint64
    for rows.Next() {
        var sid uint64
        if scanErr := rows.Scan(&sid); scanErr != nil {
            rows.Close()
            return nil, scanErr
        }
        expiredSeatIDs = append(expiredSeatIDs, sid)
    }
    if err = rows.Close(); err != nil {
        return nil, err
    }
    if len(expiredSeatIDs) == 0 {
        return []uint64{}, nil
    }
    // Delete expired holds.
    _, err = tx.ExecContext(ctx,
        `DELETE FROM seat_holds WHERE show_id = ? AND expires_at <= UTC_TIMESTAMP()`,
        showID,
    )
    if err != nil {
        return nil, err
    }
    return expiredSeatIDs, nil
}

// randomToken generates a random hexadecimal string of length n*2 bytes.
// It is used to populate the hold_token column.  The underlying call to
// crypto/rand ensures cryptographically secure random bytes.  The length
// parameter controls the number of bytes; for a 64 character hex string,
// specify 32 bytes.  On failure it returns an error.
func randomToken(n int) (string, error) {
    b := make([]byte, n)
    if _, err := rand.Read(b); err != nil {
        return "", err
    }
    return hex.EncodeToString(b), nil
}

// CreateMultipleTx inserts multiple seat_holds within the provided
// transaction.  Each hold must specify ShowID, SeatID, UserID, HoldToken
// and ExpiresAt.  The CreatedAt column is automatically set by the
// database.  The caller is responsible for committing or rolling back
// the transaction.  Passing an empty slice has no effect and returns nil.
func (r *SeatHoldRepo) CreateMultipleTx(ctx context.Context, tx *sql.Tx, holds []SeatHoldRecord) error {
    if len(holds) == 0 {
        return nil
    }
    query := `INSERT INTO seat_holds (user_id, show_id, seat_id, hold_token, expires_at) VALUES `
    args := make([]interface{}, 0, len(holds)*5)
    for i, h := range holds {
        if i > 0 {
            query += ","
        }
        query += "(?, ?, ?, ?, ?)"
        args = append(args, h.UserID, h.ShowID, h.SeatID, h.HoldToken, h.ExpiresAt.UTC().Format("2006-01-02 15:04:05"))
    }
    _, err := tx.ExecContext(ctx, query, args...)
    return err
}

// DeleteByUserAndShowTx removes all seat_holds for the specified user and show.
// It returns the seat IDs that were released so that callers may update
// associated show_seats.  The deletion occurs within the provided
// transaction; the caller must commit or roll back accordingly.
func (r *SeatHoldRepo) DeleteByUserAndShowTx(ctx context.Context, tx *sql.Tx, userID, showID uint64) ([]uint64, error) {
    // Collect seat IDs for the holds that are about to be removed.
    rows, err := tx.QueryContext(ctx, `SELECT seat_id FROM seat_holds WHERE user_id = ? AND show_id = ?`, userID, showID)
    if err != nil {
        return nil, err
    }
    var seatIDs []uint64
    for rows.Next() {
        var sid uint64
        if scanErr := rows.Scan(&sid); scanErr != nil {
            rows.Close()
            return nil, scanErr
        }
        seatIDs = append(seatIDs, sid)
    }
    if err = rows.Close(); err != nil {
        return nil, err
    }
    // Delete the holds for this user and show.
    if _, err = tx.ExecContext(ctx, `DELETE FROM seat_holds WHERE user_id = ? AND show_id = ?`, userID, showID); err != nil {
        return nil, err
    }
    return seatIDs, nil
}

// ActiveHoldsByUserAndShowTx retrieves all non-expired seat holds for a
// particular user and show.  The returned slice contains complete hold
// records.  Use this when confirming a reservation to ensure the seats
// are still held and have not expired.  The query is executed within
// the provided transaction to support locking if desired via SELECT ... FOR UPDATE.
func (r *SeatHoldRepo) ActiveHoldsByUserAndShowTx(ctx context.Context, tx *sql.Tx, userID, showID uint64) ([]SeatHoldRecord, error) {
    const q = `SELECT id, user_id, show_id, seat_id, hold_token, expires_at, created_at
               FROM seat_holds
               WHERE user_id = ? AND show_id = ? AND expires_at > UTC_TIMESTAMP()`
    // Note: not using FOR UPDATE here; callers can append "FOR UPDATE" if
    // locking is required.  Some DBs disallow FOR UPDATE with DISTINCT or JOIN.
    rows, err := tx.QueryContext(ctx, q, userID, showID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var holds []SeatHoldRecord
    for rows.Next() {
        var h SeatHoldRecord
        if err := rows.Scan(&h.ID, &h.UserID, &h.ShowID, &h.SeatID, &h.HoldToken, &h.ExpiresAt, &h.CreatedAt); err != nil {
            return nil, err
        }
        holds = append(holds, h)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return holds, nil
}

// GenerateHoldRecords builds seat hold records for the given user, show and
// seat IDs.  A new random token is generated for each seat.  The
// expiration is set to the provided timestamp.  This helper can be used
// by handlers prior to calling CreateMultipleTx.
func GenerateHoldRecords(userID, showID uint64, seatIDs []uint64, expiresAt time.Time) ([]SeatHoldRecord, error) {
    holds := make([]SeatHoldRecord, 0, len(seatIDs))
    for _, sid := range seatIDs {
        token, err := randomToken(32)
        if err != nil {
            return nil, err
        }
        holds = append(holds, SeatHoldRecord{
            UserID:    userID,
            ShowID:    showID,
            SeatID:    sid,
            HoldToken: token,
            ExpiresAt: expiresAt,
        })
    }
    return holds, nil
}