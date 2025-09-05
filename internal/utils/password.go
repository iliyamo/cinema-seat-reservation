package utils

import "golang.org/x/crypto/bcrypt"

// HashPassword returns bcrypt hash using the given cost.
func HashPassword(plain string, cost int) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// VerifyPassword safely compares bcrypt hash and plain password.
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
