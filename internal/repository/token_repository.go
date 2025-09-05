package repository

import (
	"context"
	"database/sql"
	"time"
)

// TokenRepo persists/validates refresh tokens (single 'token_hash' column).
type TokenRepo struct{ DB *sql.DB }

func NewTokenRepo(db *sql.DB) *TokenRepo { return &TokenRepo{DB: db} }

// StoreRefresh inserts a refresh token hash row.
func (r *TokenRepo) StoreRefresh(ctx context.Context, userID uint64, tokenHash string, exp time.Time) error {
	_, err := r.DB.ExecContext(ctx,
		"INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES (?,?,?)",
		userID, tokenHash, exp)
	return err
}

// ValidateRefresh returns userID if a non-revoked, non-expired token exists.
func (r *TokenRepo) ValidateRefresh(ctx context.Context, tokenHash string) (uint64, error) {
	var (
		userID    uint64
		expiresAt time.Time
		revokedAt sql.NullTime
	)
	err := r.DB.QueryRowContext(ctx,
		"SELECT user_id, expires_at, revoked_at FROM refresh_tokens WHERE token_hash=? LIMIT 1",
		tokenHash).Scan(&userID, &expiresAt, &revokedAt)
	if err != nil {
		return 0, err
	}
	if revokedAt.Valid {
		return 0, sql.ErrNoRows
	}
	if time.Now().UTC().After(expiresAt) {
		return 0, sql.ErrNoRows
	}
	return userID, nil
}

// RevokeByHash marks a token as revoked.
func (r *TokenRepo) RevokeByHash(ctx context.Context, tokenHash string) error {
	_, err := r.DB.ExecContext(ctx,
		"UPDATE refresh_tokens SET revoked_at=NOW() WHERE token_hash=? AND revoked_at IS NULL",
		tokenHash)
	return err
}

// RevokeAllForUser revokes all user's active tokens.
func (r *TokenRepo) RevokeAllForUser(ctx context.Context, userID uint64) error {
	_, err := r.DB.ExecContext(ctx,
		"UPDATE refresh_tokens SET revoked_at=NOW() WHERE user_id=? AND revoked_at IS NULL",
		userID)
	return err
}
