package utils

import (
	"crypto/rand"   // secure random
	"crypto/sha256" // hash refresh
	"encoding/hex"  // hex encoding
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AccessToken = signed JWT + expiry.
type AccessToken struct {
	Token string
	Exp   time.Time
}

// RefreshToken = raw string for client + expiry (DB stores only SHA-256 hash).
type RefreshToken struct {
	Raw string
	Exp time.Time
}

// NewAccessToken builds HS256 JWT for a user.
func NewAccessToken(secret string, userID uint64, role string, ttlMin int) (AccessToken, error) {
	exp := time.Now().UTC().Add(time.Duration(ttlMin) * time.Minute)
	claims := jwt.MapClaims{
		"sub":  userID,
		"role": role,
		"exp":  exp.Unix(),
		"iat":  time.Now().UTC().Unix(),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := t.SignedString([]byte(secret))
	if err != nil {
		return AccessToken{}, err
	}
	return AccessToken{Token: signed, Exp: exp}, nil
}

// NewRefreshToken returns a strong random token (raw) and expiry.
func NewRefreshToken(ttlDays int) (RefreshToken, error) {
	raw, err := randomHex(48) // 48 bytes -> 96 hex chars
	if err != nil {
		return RefreshToken{}, err
	}
	return RefreshToken{
		Raw: raw,
		Exp: time.Now().UTC().Add(time.Duration(ttlDays) * 24 * time.Hour),
	}, nil
}

// HashRefreshRaw returns SHA-256(raw) as hex.
func HashRefreshRaw(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// randomHex returns a hex string from n random bytes.
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
