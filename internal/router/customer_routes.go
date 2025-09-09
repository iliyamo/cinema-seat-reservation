package router

import (
    "github.com/iliyamo/cinema-seat-reservation/internal/handler"
    "github.com/iliyamo/cinema-seat-reservation/internal/middleware"
    "github.com/labstack/echo/v4"
)

// RegisterCustomer registers customer-scoped endpoints under /v1.  All routes
// require a valid JWT and the CUSTOMER role.  Customers can view seat
// status for shows, place holds on seats, release holds, confirm
// reservations and view their own reservations.
func RegisterCustomer(e *echo.Echo, h *handler.CustomerHandler, jwtSecret string) {
    g := e.Group(
        "/v1",
        middleware.JWTAuth(jwtSecret),
        middleware.RequireRole("CUSTOMER"),
    )
    // Note: GET /v1/shows/:id/seats and GET /v1/halls/:id/seats/layout are
    // registered on the public router so that guests can view seat
    // availability.  Customer-specific endpoints begin here.
    g.POST("/shows/:id/hold", h.HoldSeats)
    g.DELETE("/shows/:id/hold", h.ReleaseHolds)
    g.POST("/shows/:id/confirm", h.ConfirmSeats)
    g.GET("/my-reservations", h.ListReservations)
}