package config

// This file defines a Redis client constructor for the application.  Redis is
// used for distributed rate limiting and HTTP response caching.  The client
// parameters are loaded from environment variables.  If connection fails
// during startup, the function returns nil and callers should degrade
// gracefully by disabling caching and rate limiting.

import (
    "context"
    "crypto/tls"
    "os"
    "strconv"
    "strings"
    "time"

    "github.com/redis/go-redis/v9"
)

// NewRedisClient instantiates a Redis client using environment variables.
// Supported variables are:
//   REDIS_HOST and REDIS_PORT – hostname and port of the Redis server
//   REDIS_ADDR – host:port shorthand (takes precedence if both host/port and addr are set)
//   REDIS_PASSWORD – optional password
//   REDIS_DB – database number (default 0)
//   REDIS_TLS – enable TLS when "true" or "1"
// The returned client may be nil if a connection cannot be established.
func NewRedisClient() *redis.Client {
    host := os.Getenv("REDIS_HOST")
    port := os.Getenv("REDIS_PORT")
    addr := os.Getenv("REDIS_ADDR")
    if host != "" && port != "" {
        addr = host + ":" + port
    }
    if addr == "" {
        addr = "localhost:6379"
    }
    pwd := os.Getenv("REDIS_PASSWORD")
    dbNum := 0
    if dbStr := os.Getenv("REDIS_DB"); dbStr != "" {
        if n, err := strconv.Atoi(dbStr); err == nil {
            dbNum = n
        }
    }
    var tlsConf *tls.Config
    if tlsEnv := os.Getenv("REDIS_TLS"); strings.EqualFold(tlsEnv, "true") || tlsEnv == "1" {
        tlsConf = &tls.Config{InsecureSkipVerify: true}
    }
    client := redis.NewClient(&redis.Options{
        Addr:      addr,
        Password:  pwd,
        DB:        dbNum,
        TLSConfig: tlsConf,
    })
    // Ping the server with a short timeout.  Return nil on failure.
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    if err := client.Ping(ctx).Err(); err != nil {
        return nil
    }
    return client
}