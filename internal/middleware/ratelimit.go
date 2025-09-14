package middleware

import (
    "fmt"
    "math"
    "net/http"
    "strconv"
    "strings"
    "time"

    "github.com/labstack/echo/v4"
    "github.com/redis/go-redis/v9"

    "github.com/iliyamo/cinema-seat-reservation/internal/config"
)

func NewTokenBucket(cfg config.RateLimitConfig, rdb *redis.Client) echo.MiddlewareFunc {
    if !cfg.Enabled || rdb == nil {
        return func(next echo.HandlerFunc) echo.HandlerFunc { return func(c echo.Context) error { return next(c) } }
    }

    limiterScript := redis.NewScript(`
        local key = KEYS[1]
        local now_ms = tonumber(ARGV[1])
        local capacity = tonumber(ARGV[2])
        local refill_tokens = tonumber(ARGV[3])
        local interval_ms = tonumber(ARGV[4])
        local ttl_seconds = tonumber(ARGV[5])

        local state = redis.call('HMGET', key, 'tokens', 'last_refill_ms')
        local tokens = tonumber(state[1])
        local last_refill = tonumber(state[2])

        if tokens == nil or last_refill == nil then
            tokens = capacity
            last_refill = now_ms
        end

        if interval_ms > 0 and refill_tokens > 0 then
            local elapsed = math.max(0, now_ms - last_refill)
            local intervals = math.floor(elapsed / interval_ms)
            if intervals > 0 then
                tokens = math.min(capacity, tokens + (intervals * refill_tokens))
                last_refill = last_refill + (intervals * interval_ms)
            end
        end

        local allowed = 0
        local retry_after_ms = 0
        if tokens > 0 then
            allowed = 1
            tokens = tokens - 1
        else
            local until_next = interval_ms - (now_ms - last_refill)
            if until_next < 0 then until_next = 0 end
            retry_after_ms = until_next
        end

        redis.call('HMSET', key, 'tokens', tokens, 'last_refill_ms', last_refill, 'capacity', capacity)
        redis.call('EXPIRE', key, ttl_seconds)

        return { allowed, tokens, retry_after_ms }
    `)

    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            key := buildRateKey(cfg, c)
            now := time.Now()

            args := []interface{}{
                now.UnixMilli(),
                cfg.Capacity,
                cfg.RefillTokens,
                cfg.RefillInterval.Milliseconds(),
                int64(cfg.TTL / time.Second),
            }

            ctx := c.Request().Context()
            vals, err := limiterScript.Run(ctx, rdb, []string{key}, args...).Result()
            if err != nil {
                if cfg.Debug {
                    c.Logger().Warnf("[ratelimit] redis error for key=%s: %v", key, err)
                }
                return next(c)
            }

            allowed := false
            remaining := int64(0)
            retryMs := int64(0)

            if arr, ok := vals.([]interface{}); ok && len(arr) == 3 {
                if i, ok := arr[0].(int64); ok { allowed = (i == 1) } else { allowed = fmt.Sprint(arr[0]) == "1" }
                remaining = asInt64(arr[1])
                retryMs = asInt64(arr[2])
            } else {
                if cfg.Debug {
                    c.Logger().Warnf("[ratelimit] unexpected script result for key=%s: %#v", key, vals)
                }
                return next(c)
            }

            c.Response().Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.Capacity))
            c.Response().Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))

            if !allowed {
                secs := int(math.Ceil(float64(retryMs) / 1000.0))
                if secs < 0 { secs = 0 }
                c.Response().Header().Set("Retry-After", strconv.Itoa(secs))
                if cfg.Debug {
                    c.Logger().Infof("[ratelimit] block key=%s remaining=%d retry=%dms", key, remaining, retryMs)
                }
                return c.JSON(http.StatusTooManyRequests, map[string]any{
                    "error":       "too_many_requests",
                    "message":     "rate limit exceeded",
                    "retry_after": secs,
                })
            }

            if cfg.Debug {
                c.Response().Header().Set("X-RateLimit-Key", key)
            }
            return next(c)
        }
    }
}

func asInt64(v interface{}) int64 {
    switch t := v.(type) {
    case int64: return t
    case int32: return int64(t)
    case int: return int64(t)
    case float64: return int64(t)
    case float32: return int64(t)
    case string:
        if n, err := strconv.ParseInt(t, 10, 64); err == nil { return n }
    }
    return 0
}

func buildRateKey(cfg config.RateLimitConfig, c echo.Context) string {
    parts := []string{cfg.Prefix}
    strategy := strings.ToLower(cfg.KeyStrategy)
    ip := c.RealIP()
    if ip == "" { ip = "unknown" }
    uid := currentUserID(c)
    route := c.Request().Method + " " + c.Path()

    switch strategy {
    case "ip":
        parts = append(parts, "ip", ip)
    case "user":
        parts = append(parts, "user", uid)
    case "route":
        parts = append(parts, "route", route)
    case "ip_user":
        parts = append(parts, "ip", ip, "user", uid)
    case "ip_route":
        parts = append(parts, "ip", ip, "route", route)
    case "user_route":
        parts = append(parts, "user", uid, "route", route)
    default:
        parts = append(parts, "ip", ip, "user", uid, "route", route)
    }
    return strings.Join(parts, ":")
}

func currentUserID(c echo.Context) string {
    if v := c.Get("user_id"); v != nil {
        if s, ok := v.(string); ok && s != "" { return s }
    }
    if v := c.Get("userID"); v != nil {
        if s, ok := v.(string); ok && s != "" { return s }
    }
    return "anon"
}
