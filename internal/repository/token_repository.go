package repository // declare the repository package; contains data access logic

import (
    "context"      // context provides cancellation and timeouts for DB operations
    "database/sql" // SQL database interactions and types
    "time"         // time for expiry and revocation timestamps
)

// TokenRepo persists and validates refresh tokens in the database.  It holds
// a pointer to a sql.DB used to execute queries.  Refresh tokens are stored
// hashed; only the hash and expiry timestamps are saved.  Revocation is
// recorded via a nullable timestamp column.
type TokenRepo struct{ DB *sql.DB }

// NewTokenRepo constructs a new TokenRepo given a database handle.
func NewTokenRepo(db *sql.DB) *TokenRepo { return &TokenRepo{DB: db} }

// StoreRefresh inserts a row containing the hashed refresh token, the user ID
// and the expiry time.  It returns any error from the underlying Exec call.
func (r *TokenRepo) StoreRefresh(ctx context.Context, userID uint64, tokenHash string, exp time.Time) error {
    _, err := r.DB.ExecContext(ctx,
        "INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES (?,?,?)",
        userID, tokenHash, exp)
    return err
}

// ValidateRefresh checks that a given token hash exists, is not revoked and
// has not expired.  It returns the associated user ID or sql.ErrNoRows if
// validation fails.  Clients should treat sql.ErrNoRows as a generic
// invalid token error.
func (r *TokenRepo) ValidateRefresh(ctx context.Context, tokenHash string) (uint64, error) {
    var (
        userID    uint64      // will hold the associated user ID
        expiresAt time.Time   // expiry timestamp retrieved from DB
        revokedAt sql.NullTime // nullable timestamp; valid if revoked
    )
    // Query for the first row matching the token hash.  If not found,
    // QueryRowContext returns sql.ErrNoRows which the caller should handle.
    err := r.DB.QueryRowContext(ctx,
        "SELECT user_id, expires_at, revoked_at FROM refresh_tokens WHERE token_hash=? LIMIT 1",
        tokenHash).Scan(&userID, &expiresAt, &revokedAt)
    if err != nil {
        return 0, err
    }
    // If revokedAt is set, the token has been revoked and is no longer valid.
    if revokedAt.Valid {
        return 0, sql.ErrNoRows
    }
    // If the current time is after the expiry, treat as invalid.
    if time.Now().UTC().After(expiresAt) {
        return 0, sql.ErrNoRows
    }
    // On success return the user ID.
    return userID, nil
}

// RevokeByHash marks a specific refresh token as revoked by setting the
// revoked_at timestamp.  Only tokens that have not already been revoked
// will be updated.  Returns any error from Exec.
func (r *TokenRepo) RevokeByHash(ctx context.Context, tokenHash string) error {
    _, err := r.DB.ExecContext(ctx,
        "UPDATE refresh_tokens SET revoked_at=NOW() WHERE token_hash=? AND revoked_at IS NULL",
        tokenHash)
    return err
}

// RevokeAllForUser revokes all active refresh tokens for a given user by
// setting revoked_at on all rows where revoked_at is NULL.  Returns any
// execution error.
func (r *TokenRepo) RevokeAllForUser(ctx context.Context, userID uint64) error {
    _, err := r.DB.ExecContext(ctx,
        "UPDATE refresh_tokens SET revoked_at=NOW() WHERE user_id=? AND revoked_at IS NULL",
        userID)
    return err
}
