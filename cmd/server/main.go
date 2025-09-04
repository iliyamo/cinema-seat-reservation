package main // Entry point package

import (
	"log" // Logging library

	"github.com/iliyamo/cinema-seat-reservation/internal/config" // Internal config loader
	"github.com/iliyamo/cinema-seat-reservation/internal/router" // Internal router setup
	"github.com/labstack/echo/v4"                                // Echo web framework
)

func main() {
	cfg := config.Load()     // Load environment config
	e := echo.New()          // Create Echo instance
	router.RegisterRoutes(e) // Register application routes

	addr := ":" + cfg.Port                                // Address string with port
	log.Printf("listening on %s (env=%s)", addr, cfg.Env) // Print startup info

	if err := e.Start(addr); err != nil { // Start HTTP server
		log.Fatal(err) // Log and exit if server fails
	}
}
