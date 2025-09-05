package main // declare the main package; entry point of the application

import (
    "log" // log package for logging messages during startup and runtime
    "os"  // os provides functions for interacting with the environment and filesystem

    "github.com/joho/godotenv" // godotenv loads environment variables from .env files
    "github.com/labstack/echo/v4" // echo is the web framework used to create the HTTP server

    "github.com/iliyamo/cinema-seat-reservation/internal/config"     // import configuration loader
    "github.com/iliyamo/cinema-seat-reservation/internal/database"   // import database connection helper
    "github.com/iliyamo/cinema-seat-reservation/internal/handler"    // import handlers for business logic
    "github.com/iliyamo/cinema-seat-reservation/internal/repository" // import repositories for persistence
    "github.com/iliyamo/cinema-seat-reservation/internal/router"     // import router to register routes
)

// loadDotEnv attempts to load environment variables from a list of potential
// .env files.  It walks up the directory tree and loads the first file it
// finds.  If no file is found it logs a message and environment variables
// must be provided by the operating system.
func loadDotEnv() {
    paths := []string{".env", "../.env", "../../.env"} // candidate locations for .env files starting from CWD upwards
    for _, p := range paths {                                // iterate over each candidate path
        if _, err := os.Stat(p); err == nil {                // check if the file exists
            _ = godotenv.Overload(p)                        // load variables from the file, overriding existing ones
            log.Printf("env: loaded %s", p)                 // log which file was loaded
            return                                          // stop searching after loading one file
        }
    }
    log.Println("env: no .env found; expecting system envs") // if no file found, log that we rely on system env
}

// main is the application entry point.  It performs setup of configuration,
// database connections, route registration and starts the HTTP server.
func main() {
    loadDotEnv()                            // load environment variables from disk if available

    cfg := config.Load()                    // read required configuration values from the environment; will exit on failure

    db, err := database.Open(cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBName) // open a database connection using the config values
    if err != nil {                            // handle any connection error
        log.Fatalf("db connect error: %v", err) // abort the program with an error message
    }
    defer db.Close()                          // ensure the database connection is closed when main exits
    log.Println("db connected")               // log that the connection succeeded

    e := echo.New()                           // create a new Echo instance which will serve HTTP requests
    // register basic routes that do not require authentication
    router.RegisterRoutes(e)

    // initialise repositories and handlers for auth endpoints
    ur := repository.NewUserRepo(db)          // create a user repository using the open database
    tr := repository.NewTokenRepo(db)         // create a token repository using the same database
    authH := handler.NewAuthHandler(cfg, ur, tr) // create an authentication handler with config and repositories
    // register auth routes with the JWT secret; this adds both public and protected routes
    router.RegisterAuth(e, authH, cfg.JWTSecret)

    addr := ":" + cfg.Port                    // build the address string using the configured port
    log.Printf("listening on %s (env=%s)", addr, cfg.Env) // log where the server is about to start
    log.Fatal(e.Start(addr))                   // start serving HTTP requests and exit if the server returns an error
}
