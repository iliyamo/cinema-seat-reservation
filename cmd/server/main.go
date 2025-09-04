package main // Entry point package

import (
	"log" // Logging

	"github.com/joho/godotenv" // Load .env (dev/local)

	"github.com/iliyamo/cinema-seat-reservation/internal/config" // Config loader
	"github.com/iliyamo/cinema-seat-reservation/internal/router" // Router setup
	"github.com/labstack/echo/v4"                                // Echo web framework
)

func main() {
	// Load .env if present (ignore error in dev/local)
	if err := godotenv.Load(); err != nil { // Try to load .env
		log.Println("info: .env not found; using defaults/env") // Non-fatal notice
	}

	cfg := config.Load() // Load environment config

	e := echo.New()          // Create Echo instance
	router.RegisterRoutes(e) // Register application routes

	addr := ":" + cfg.Port                                // Address string with port
	log.Printf("listening on %s (env=%s)", addr, cfg.Env) // Print startup info

	if err := e.Start(addr); err != nil { // Start HTTP server
		log.Fatal(err) // Log and exit if server fails
	}
}
