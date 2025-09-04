package config // Config package

import "os" // OS package for env variables

type Config struct {
	Env  string // Environment (dev, prod, etc.)
	Port string // Server port
}

func Load() Config {
	return Config{
		Env:  getenv("APP_ENV", "dev"), // Load APP_ENV or default "dev"
		Port: getenv("PORT", "8080"),   // Load PORT or default "8080"
	}
}

func getenv(k, def string) string { // Helper to read env with default
	if v := os.Getenv(k); v != "" { // If variable is set
		return v // Return its value
	}
	return def // Otherwise return default
}
