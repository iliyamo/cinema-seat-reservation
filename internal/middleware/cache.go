package middleware

import (
    "bytes"
    "context"
    "crypto/sha1"
    "encoding/binary"
    "encoding/json"
    "fmt"
    "net/http"
    "strings"
    "time"

    "github.com/labstack/echo/v4"
    "github.com/redis/go-redis/v9"

    "github.com/iliyamo/cinema-seat-reservation/internal/config"
)

// captureWriter captures response body/status while forwarding to the client.
type captureWriter struct {
    http.ResponseWriter
    status int
    buf    bytes.Buffer
    size   int64
    limit  int64
}
func (cw *captureWriter) WriteHeader(code int) { cw.status = code; cw.ResponseWriter.WriteHeader(code) }
func (cw *captureWriter) Write(b []byte) (int, error) {
    if cw.limit <= 0 || cw.size < cw.limit {
        remain := cw.limit - cw.size
        if cw.limit <= 0 {
            cw.buf.Write(b)
        } else if remain > 0 {
            if int64(len(b)) <= remain {
                cw.buf.Write(b)
            } else {
                cw.buf.Write(b[:remain])
            }
        }
        cw.size += int64(len(b))
    }
    return cw.ResponseWriter.Write(b)
}

// Build a stable cache key honoring prefix/strategy.
func cacheKeyFrom(cfg config.CacheConfig, c echo.Context) string {
    r := c.Request()
    method := r.Method
    route := c.Path()
    query := r.URL.RawQuery

    parts := []string{cfg.Prefix}
    switch strings.ToLower(cfg.KeyStrategy) {
    case "route":
        parts = append(parts, "route", route)
    case "method_route":
        parts = append(parts, "method", method, "route", route)
    case "method_route_query":
        parts = append(parts, "method", method, "route", route, "q", query)
    default: // "route_query"
        parts = append(parts, "route", route, "q", query)
    }

    tail := strings.Join(parts[1:], ":")
    sum := sha1.Sum([]byte(tail))
    return fmt.Sprintf("%s:%x", parts[0], sum[:])
}

// encodePayload packs: [4 bytes status][4 bytes headerLen][headerJSON][body]
func encodePayload(status int, header http.Header, body []byte) ([]byte, error) {
    hdrJSON, err := json.Marshal(header)
    if err != nil {
        return nil, err
    }
    total := 4 + 4 + len(hdrJSON) + len(body)
    out := make([]byte, total)
    binary.BigEndian.PutUint32(out[0:4], uint32(status))
    binary.BigEndian.PutUint32(out[4:8], uint32(len(hdrJSON)))
    copy(out[8:8+len(hdrJSON)], hdrJSON)
    copy(out[8+len(hdrJSON):], body)
    return out, nil
}

func decodePayload(bs []byte) (status int, header http.Header, body []byte, ok bool) {
    if len(bs) < 8 {
        return 0, nil, nil, false
    }
    status = int(binary.BigEndian.Uint32(bs[0:4]))
    hlen := int(binary.BigEndian.Uint32(bs[4:8]))
    if 8+hlen > len(bs) || hlen < 0 {
        return 0, nil, nil, false
    }
    var hdr http.Header
    if hlen > 0 {
        if err := json.Unmarshal(bs[8:8+hlen], &hdr); err != nil {
            return 0, nil, nil, false
        }
    } else {
        hdr = make(http.Header)
    }
    body = bs[8+hlen:]
    return status, hdr, body, true
}

// NewRedisCache stores headers + body so clients see identical formatting (e.g., pretty JSON) as original.
func NewRedisCache(cfg config.CacheConfig, rdb *redis.Client) echo.MiddlewareFunc {
    if !cfg.Enabled || rdb == nil {
        return func(next echo.HandlerFunc) echo.HandlerFunc { return func(c echo.Context) error { return next(c) } }
    }
    ttl := cfg.TTL
    if ttl <= 0 { ttl = 5 * time.Minute } // sane default longer TTL

    maxBody := int64(cfg.MaxBodyBytes)

    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            if !cfg.Methods[strings.ToUpper(c.Request().Method)] {
                return next(c)
            }

            ctx := c.Request().Context()
            key := cacheKeyFrom(cfg, c)

            // Try get from Redis
            if bs, err := rdb.Get(ctx, key).Bytes(); err == nil && len(bs) >= 8 {
                if status, hdr, body, ok := decodePayload(bs); ok {
                    // Restore headers (except hop-by-hop)
                    for k, vals := range hdr {
                        // X-Cache will be set below; skip Content-Length (Echo will handle)
                        if strings.EqualFold(k, "Content-Length") { continue }
                        for _, v := range vals {
                            c.Response().Header().Add(k, v)
                        }
                    }
                    c.Response().Header().Set("X-Cache", "HIT")
                    c.Response().WriteHeader(status)
                    if len(body) > 0 {
                        _, _ = c.Response().Write(body)
                    }
                    return nil
                }
            }

            // Miss: capture
            cw := &captureWriter{ResponseWriter: c.Response().Writer, status: http.StatusOK, limit: maxBody}
            c.Response().Writer = cw
            c.Response().Header().Set("X-Cache", "MISS")

            if err := next(c); err != nil {
                return err
            }

            if cw.status == http.StatusOK {
                // Copy headers from response
                hdr := make(http.Header, len(c.Response().Header()))
                for k, vals := range c.Response().Header() {
                    vv := make([]string, len(vals))
                    copy(vv, vals)
                    hdr[k] = vv
                }
                body := cw.buf.Bytes()
                if maxBody > 0 && int64(len(body)) > maxBody {
                    body = body[:maxBody]
                }
                if payload, err := encodePayload(cw.status, hdr, body); err == nil {
                    _ = rdb.SetEx(context.Background(), key, payload, ttl).Err()
                }
            }
            return nil
        }
    }
}
