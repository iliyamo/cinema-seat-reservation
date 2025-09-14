package config

import (
    "os"
    "strconv"
    "strings"
    "time"
)

// CacheConfig defines settings for the response cache middleware.
// When Enabled is false or no Redis client is configured, caching will be disabled.
// Methods lists the HTTP methods to cache (e.g. GET, HEAD).  TTL defines the
// lifetime of cache entries.  KeyStrategy determines which parts of the request
// contribute to the cache key.  Prefix and MaxBodyBytes allow control over
// namespacing and the maximum size of responses to cache.
type CacheConfig struct {
    Enabled      bool
    Methods      map[string]bool
    TTL          time.Duration
    KeyStrategy  string
    Prefix       string
    MaxBodyBytes int
}

// LoadCacheConfig reads environment variables to build a CacheConfig.  Defaults
// are used when variables are not set.  All methods are upper-cased.
func LoadCacheConfig() CacheConfig {
    return CacheConfig{
        Enabled:      getenv("CACHE_ENABLED", "true") == "true",
        Methods:      parseMethods(getenv("CACHE_METHODS", "GET")),
        TTL:          parseDur(getenv("CACHE_TTL", "30s")),
        KeyStrategy:  getenv("CACHE_KEY_STRATEGY", "route_query"),
        Prefix:       getenv("CACHE_PREFIX", "cache"),
        MaxBodyBytes: atoi(getenv("CACHE_MAX_BODY_BYTES", "1048576")),
    }
}

func parseMethods(s string) map[string]bool {
    m := map[string]bool{}
    for _, p := range strings.Split(s, ",") {
        p = strings.TrimSpace(strings.ToUpper(p))
        if p != "" {
            m[p] = true
        }
    }
    return m
}

// Helper functions reused from redis.go and ratelimit.go
func getenv(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}

func atoi(s string) int {
    i, _ := strconv.Atoi(s)
    return i
}

func parseDur(s string) time.Duration {
    d, err := time.ParseDuration(s)
    if err != nil {
        return time.Second
    }
    return d
}