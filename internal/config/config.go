package config // Config package

import (
	"os"      // Read env vars (optional)
	"strconv" // Parse ints
)

type Config struct {
	Env        string // Environment name
	Port       string // HTTP port
	DBUser     string // DB user
	DBPass     string // DB password
	DBHost     string // DB host
	DBPort     string // DB port
	DBName     string // DB name
	JWTSecret  string // HS256 secret
	AccessTTL  int    // Access token TTL (minutes)
	RefreshTTL int    // Refresh token TTL (hours)
	BcryptCost int    // bcrypt cost factor
}

// Hard-coded defaults (dev-only). You can override via env, but not required.
const (
	defEnv        = "dev"           // Default env
	defPort       = "8080"          // Default HTTP port
	defDBUser     = "csr"           // Default DB user
	defDBPass     = "csrpass"       // Default DB password
	defDBHost     = "localhost"     // Default DB host
	defDBPort     = "3306"          // Default DB port
	defDBName     = "cinema"        // Default DB name
	defJWTSecret  = "dev-change-me" // Default JWT secret (change in prod)
	defAccessTTL  = 15              // 15 minutes
	defRefreshTTL = 168             // 168 hours = 7 days
	defBcryptCost = 12              // bcrypt cost
)

func Load() Config {
	return Config{
		Env:        get("APP_ENV", defEnv),                   // env or code default
		Port:       get("PORT", defPort),                     // env or code default
		DBUser:     get("DB_USER", defDBUser),                // env or code default
		DBPass:     get("DB_PASS", defDBPass),                // env or code default
		DBHost:     get("DB_HOST", defDBHost),                // env or code default
		DBPort:     get("DB_PORT", defDBPort),                // env or code default
		DBName:     get("DB_NAME", defDBName),                // env or code default
		JWTSecret:  get("JWT_SECRET", defJWTSecret),          // env or code default
		AccessTTL:  geti("ACCESS_TTL_MIN", defAccessTTL),     // env or code default
		RefreshTTL: geti("REFRESH_TTL_HOURS", defRefreshTTL), // env or code default
		BcryptCost: geti("BCRYPT_COST", defBcryptCost),       // env or code default
	}
}

func get(k, def string) string { // Read string env with fallback
	if v := os.Getenv(k); v != "" { // If present in env
		return v // Use env
	}
	return def // Else use code default
}

func geti(k string, def int) int { // Read int env with fallback
	if v := os.Getenv(k); v != "" { // If present in env
		if n, err := strconv.Atoi(v); err == nil { // Parse int
			return n // Use env
		}
	}
	return def // Else use code default
}
