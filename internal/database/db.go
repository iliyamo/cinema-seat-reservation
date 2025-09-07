package database // package database provides helpers for connecting to the DB

import (
    "context"      // context allows us to set timeouts when pinging the database
    "database/sql" // generic database interface from the standard library
    "fmt"          // string formatting utilities
    "time"         // time types for setting connection lifetimes

    _ "github.com/go-sql-driver/mysql" // import MySQL driver anonymously to register it with database/sql
)

// Open connects to a MySQL database using the provided credentials and
// connection parameters.  It sets reasonable connection pool settings and
// verifies the connection by performing a ping with a timeout.  On
// successful connection it returns a *sql.DB ready for use; otherwise an
// error is returned.
func Open(user, pass, host, port, name string) (*sql.DB, error) {
    // Build the authentication part of the DSN.  If a password is provided,
    // include it in the DSN; otherwise only use the username.
    auth := user
    if pass != "" {
        auth = fmt.Sprintf("%s:%s", user, pass)
    }
    // Construct the DSN (Data Source Name).  parseTime=true tells the MySQL
    // driver to parse DATETIME/TIMESTAMP fields into time.Time values.  loc=UTC
    // ensures that times are interpreted in UTC.
    dsn := fmt.Sprintf("%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=UTC",
        auth, host, port, name)

    // Open a new database handle.  sql.Open does not establish connections
    // immediately; it validates the arguments and prepares the handle.
    db, err := sql.Open("mysql", dsn)
    if err != nil {
        return nil, err
    }

    // Configure connection pooling.  Set the maximum number of open and
    // idle connections and limit the lifetime of connections to 30 minutes.
    db.SetMaxOpenConns(25)
    db.SetMaxIdleConns(25)
    db.SetConnMaxLifetime(30 * time.Minute)

    // Verify the database connection by pinging it with a timeout.  Use a
    // context with a 5‑second deadline to avoid hanging on broken networks.
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := db.PingContext(ctx); err != nil {
        return nil, err
    }
    return db, nil
}
