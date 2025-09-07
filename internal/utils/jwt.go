package utils // package utils provides helper functions for token creation and hashing

import (
    "crypto/rand"   // secure random number generation
    "crypto/sha256" // SHA‑256 hashing for refresh tokens
    "encoding/hex"  // hex encoding and decoding functions
    "time"          // time utilities for generating expirations

    "github.com/golang-jwt/jwt/v5" // JWT library for creating signed tokens
)

// AccessToken represents a signed JWT access token along with its expiry.
// The Token field contains the JWT string.  Exp stores the expiration
// timestamp as a time.Time.  Access tokens are short‑lived and encoded
// in the Authorization header when calling protected endpoints.
type AccessToken struct {
    Token string    // the serialized JWT string
    Exp   time.Time // the UTC expiration time
}

// RefreshToken represents a long‑lived token used to obtain new access tokens.
// The Raw field contains the raw token string returned to the client.  The Exp
// field records when it expires.  In the database only a SHA‑256 hash of the
// raw string is stored for security reasons.
type RefreshToken struct {
    Raw string    // raw token string returned to the client
    Exp time.Time // UTC expiration time
}

// NewAccessToken builds and signs an HS256 JWT for a user.  It takes the
// signing secret, the user ID, the user's role, and a TTL in minutes.  It
// returns an AccessToken structure containing the signed token and its
// expiration time.  The JWT includes standard claims: subject (sub), role,
// expiration (exp) and issued at (iat).
func NewAccessToken(secret string, userID uint64, role string, ttlMin int) (AccessToken, error) {
    // Calculate the expiration time by adding the TTL to the current UTC time.
    exp := time.Now().UTC().Add(time.Duration(ttlMin) * time.Minute)
    // Construct the JWT claims.  Using MapClaims allows arbitrary key/value
    // pairs.  We set sub to the user ID, role to the user's role, exp to
    // the expiration Unix timestamp, and iat to the issued at time.
    claims := jwt.MapClaims{
        "sub":  userID,
        "role": role,
        "exp":  exp.Unix(),
        "iat":  time.Now().UTC().Unix(),
    }
    // Create a new token object specifying the signing method (HS256) and
    // include the claims.
    t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    // Sign the token with the provided secret and obtain the string form.  If
    // signing fails, return the error and a zero AccessToken.
    signed, err := t.SignedString([]byte(secret))
    if err != nil {
        return AccessToken{}, err
    }
    return AccessToken{Token: signed, Exp: exp}, nil
}

// NewRefreshToken returns a cryptographically secure random token (raw) and
// its expiration time.  Refresh tokens live longer than access tokens and
// are used to obtain new access tokens.  The ttlDays parameter controls
// how many days the refresh token is valid.
func NewRefreshToken(ttlDays int) (RefreshToken, error) {
    // Generate a random 48‑byte string and encode it as hex (96 characters).
    raw, err := randomHex(48) // 48 bytes -> 96 hex chars
    if err != nil {
        return RefreshToken{}, err
    }
    return RefreshToken{
        Raw: raw,
        // Set the expiration by adding the specified number of days to the current UTC time.
        Exp: time.Now().UTC().Add(time.Duration(ttlDays) * 24 * time.Hour),
    }, nil
}

// HashRefreshRaw returns the SHA‑256 hash of the raw refresh token as a hex
// string.  Storing only the hash in the database prevents attackers from
// using stolen database entries to refresh sessions.
func HashRefreshRaw(raw string) string {
    // Compute the SHA‑256 digest of the raw bytes.
    sum := sha256.Sum256([]byte(raw))
    // Convert the binary digest to a hex string.
    return hex.EncodeToString(sum[:])
}

// randomHex returns a hex‑encoded string generated from n bytes of
// cryptographically secure random data.  It is used to produce refresh
// tokens.  If the random number generator fails, an error is returned.
func randomHex(n int) (string, error) {
    // Allocate a slice of n bytes.
    buf := make([]byte, n)
    // Fill the slice with secure random data.  rand.Read returns the number
    // of bytes read and an error.  We ignore the count since we request
    // exactly n bytes.
    if _, err := rand.Read(buf); err != nil {
        return "", err
    }
    // Convert the random bytes to a hex string and return.
    return hex.EncodeToString(buf), nil
}
