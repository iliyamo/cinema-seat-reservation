package config // package config loads application configuration from environment variables

import (
    "log"      // log is used to report configuration errors and halt execution
    "os"       // os provides access to environment variables
    "strconv"  // strconv converts strings to other types
)

// Config holds all runtime configuration values.  Each field corresponds to
// an environment variable.  The types reflect how the values are used in
// the application: strings for identifiers and secrets, ints for durations and costs.
type Config struct {
    Env            string // application environment (e.g. "dev", "prod")
    Port           string // HTTP port to listen on
    DBUser         string // database username
    DBPass         string // database password (optional)
    DBHost         string // database host address
    DBPort         string // database port number
    DBName         string // database name
    JWTSecret      string // secret used to sign JWTs
    AccessTTLMin   int    // access token time‑to‑live in minutes
    RefreshTTLDays int    // refresh token time‑to‑live in days
    BcryptCost     int    // bcrypt cost for password hashing
}

// Load reads configuration values from environment variables and returns a
// Config.  Required variables are enforced by must() and missing values
// cause the program to exit with a fatal log message.
func Load() Config {
    return Config{
        Env:            must("APP_ENV"),             // environment (dev/test/prod)
        Port:           must("APP_PORT"),            // port to bind the HTTP server
        DBUser:         must("DB_USER"),             // database user
        DBPass:         os.Getenv("DB_PASS"),        // database password (empty allowed)
        DBHost:         must("DB_HOST"),             // database host
        DBPort:         must("DB_PORT"),             // database port
        DBName:         must("DB_NAME"),             // database name
        JWTSecret:      must("JWT_SECRET"),          // secret used for signing JWTs
        AccessTTLMin:   mustInt("ACCESS_TOKEN_TTL_MIN"),   // TTL for access tokens in minutes
        RefreshTTLDays: mustInt("REFRESH_TOKEN_TTL_DAYS"), // TTL for refresh tokens in days
        BcryptCost:     mustInt("BCRYPT_COST"),      // bcrypt cost factor
    }
}

// must retrieves the value of a required environment variable.  If the
// variable is unset or empty, the application logs a fatal error and exits.
func must(key string) string {
    v, ok := os.LookupEnv(key)
    if !ok || v == "" {
        log.Fatalf("missing required env var: %s", key)
    }
    return v
}

// mustInt is like must() but converts the retrieved string into an integer.
// If conversion fails, the application logs a fatal error and exits.
func mustInt(key string) int {
    s := must(key)
    n, err := strconv.Atoi(s)
    if err != nil {
        log.Fatalf("invalid int for %s: %q", key, s)
    }
    return n
}
