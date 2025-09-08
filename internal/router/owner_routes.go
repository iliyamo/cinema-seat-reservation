package router // router defines how HTTP routes are registered for the API

import (
	"github.com/iliyamo/cinema-seat-reservation/internal/handler"    // owner handlers
	"github.com/iliyamo/cinema-seat-reservation/internal/middleware" // JWT + role middlewares
	"github.com/labstack/echo/v4"
)

// RegisterOwner registers OWNER-scoped endpoints under /v1.
// All routes require a valid JWT and OWNER role.
func RegisterOwner(e *echo.Echo, o *handler.OwnerHandler, jwtSecret string) {
	// Attach middlewares at group construction time for clarity.
	g := e.Group(
		"/v1",
		middleware.JWTAuth(jwtSecret),
		middleware.RequireRole("OWNER"),
	)

	// ---- Cinemas ----
    g.POST("/cinemas", o.CreateCinema)
    // NOTE: Listing cinemas is handled by the public browse API.  Owner‑scoped
    // list endpoints have been removed to avoid route conflicts with the
    // public /v1/cinemas handler.
    // g.GET("/cinemas", o.ListCinemas)
    g.PUT("/cinemas/:id", o.UpdateCinema)
    g.PATCH("/cinemas/:id", o.UpdateCinema) // allow partial/semantic updates via PATCH as well
    g.DELETE("/cinemas/:id", o.DeleteCinema)

	// ---- Halls ----
    g.POST("/halls", o.CreateHall)
    g.PUT("/halls/:id", o.UpdateHall)
    g.PATCH("/halls/:id", o.UpdateHall)
    // NOTE: Listing halls by cinema is provided by the public API (GET /v1/cinemas/:id/halls).
    // g.GET("/cinemas/:cinema_id/halls", o.ListHallsInCinema)
    g.DELETE("/halls/:id", o.DeleteHall)

	// ---- Seats ----
	g.POST("/seats", o.CreateSeat)
	g.PUT("/seats/:id", o.UpdateSeat)   // returns 200 with updated seat in handler
	g.PATCH("/seats/:id", o.UpdateSeat) // alias for clients that use PATCH
	g.DELETE("/seats/:id", o.DeleteSeat)

    // ---- Shows ----
    g.POST("/shows", o.CreateShow)
    // allow full/partial updates to show properties
    g.PUT("/shows/:id", o.UpdateShow)
    g.PATCH("/shows/:id", o.UpdateShow)
    // NOTE: Listing shows in a hall is handled by the public API at /v1/halls/:id/shows.
    // g.GET("/halls/:hall_id/shows", o.ListShowsInHall)
    g.DELETE("/shows/:id", o.DeleteShow)

    // ---- Seats
    g.GET("/halls/:hall_id/seats", o.ListSeatsFlat)          // flat seat list by hall id
    g.GET("/halls/:hall_id/seats/layout", o.ListSeatsLayout) // seat layout grouped by row and number

}
