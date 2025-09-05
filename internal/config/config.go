package config

import (
	"log"
	"os"
	"strconv"
)

type Config struct {
	Env            string
	Port           string
	DBUser         string
	DBPass         string
	DBHost         string
	DBPort         string
	DBName         string
	JWTSecret      string
	AccessTTLMin   int
	RefreshTTLDays int
	BcryptCost     int
}

func Load() Config {
	return Config{
		Env:            must("APP_ENV"),
		Port:           must("APP_PORT"),
		DBUser:         must("DB_USER"),
		DBPass:         os.Getenv("DB_PASS"),
		DBHost:         must("DB_HOST"),
		DBPort:         must("DB_PORT"),
		DBName:         must("DB_NAME"),
		JWTSecret:      must("JWT_SECRET"),
		AccessTTLMin:   mustInt("ACCESS_TOKEN_TTL_MIN"),
		RefreshTTLDays: mustInt("REFRESH_TOKEN_TTL_DAYS"),
		BcryptCost:     mustInt("BCRYPT_COST"),
	}
}

func must(key string) string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		log.Fatalf("missing required env var: %s", key)
	}
	return v
}

func mustInt(key string) int {
	s := must(key)
	n, err := strconv.Atoi(s)
	if err != nil {
		log.Fatalf("invalid int for %s: %q", key, s)
	}
	return n
}
