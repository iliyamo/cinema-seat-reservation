package utils // package utils contains helper functions unrelated to business logic

import "golang.org/x/crypto/bcrypt" // import the bcrypt library for hashing and verifying passwords

// HashPassword takes a plaintext password and a bcrypt cost and returns the
// resulting hash as a string.  Bcrypt salts and hashes the password
// internally.  If hashing fails, the error is returned along with an empty
// string.
func HashPassword(plain string, cost int) (string, error) {
    // Convert the plaintext password to a byte slice and generate the hash
    // using the provided cost.  Higher cost means more computation time.
    b, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
    if err != nil {
        return "", err
    }
    // Convert the hashed bytes back to a string before returning.
    return string(b), nil
}

// VerifyPassword compares a stored bcrypt hash against a plaintext password.
// It returns true if the password matches, otherwise false.  Bcrypt's
// CompareHashAndPassword performs a constant‑time comparison to mitigate
// timing attacks.
func VerifyPassword(hash, plain string) bool {
    // bcrypt.CompareHashAndPassword returns nil on success, otherwise an error.
    return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
