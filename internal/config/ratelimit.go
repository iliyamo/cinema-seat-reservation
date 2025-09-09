package config

import (
    "os"
    "strconv"
    "time"
)

type RateLimitConfig struct {
    Enabled        bool
    Capacity       int
    RefillTokens   int
    RefillInterval time.Duration
    TTL            time.Duration
    KeyStrategy    string
    Prefix         string
    Debug          bool
}

func LoadRateLimitConfig() RateLimitConfig {
    def := RateLimitConfig{
        Enabled:        envBool("RATE_LIMIT_ENABLED", true),
        Capacity:       envInt("RATE_LIMIT_CAPACITY", 60),
        RefillTokens:   envInt("RATE_LIMIT_REFILL_TOKENS", 1),
        RefillInterval: envDur("RATE_LIMIT_REFILL_INTERVAL", time.Second),
        TTL:            envDur("RATE_LIMIT_TTL", 10*time.Minute),
        KeyStrategy:    envStr("RATE_LIMIT_KEY_STRATEGY", "ip_user_route"),
        Prefix:         envStr("RATE_LIMIT_PREFIX", "rl"),
        Debug:          envBool("RATE_LIMIT_DEBUG", false),
    }
    if b := envInt("RATE_LIMIT_BURST", -1); b > 0 { def.Capacity = b }
    if every := envDur("RATE_LIMIT_REFILL_EVERY", 0); every > 0 {
        def.RefillTokens = 1
        def.RefillInterval = every
    }
    if def.Capacity < 1 { def.Capacity = 1 }
    if def.RefillTokens < 1 { def.RefillTokens = 1 }
    if def.RefillInterval <= 0 { def.RefillInterval = time.Second }
    minTTL := 5 * def.RefillInterval
    if def.TTL < minTTL { def.TTL = minTTL }
    return def
}

func envStr(k, d string) string { if v := os.Getenv(k); v != "" { return v }; return d }
func envBool(k string, d bool) bool {
    v := os.Getenv(k)
    if v == "" { return d }
    switch v {
    case "1","true","TRUE","True","yes","YES","on","ON": return true
    case "0","false","FALSE","False","no","NO","off","OFF": return false
    }
    return d
}
func envInt(k string, d int) int {
    v := os.Getenv(k); if v == "" { return d }
    if n, err := strconv.Atoi(v); err == nil { return n }
    return d
}
func envDur(k string, d time.Duration) time.Duration {
    v := os.Getenv(k); if v == "" { return d }
    if dur, err := time.ParseDuration(v); err == nil { return dur }
    return d
}
