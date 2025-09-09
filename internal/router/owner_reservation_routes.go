package router

// This file registers owner-specific routes for managing reservations.  The
// routes defined here allow owners to list, view and cancel
// reservations on shows that belong to their halls.  They are
// separate from the generic owner routes to keep concerns isolated.

import (
    "github.com/iliyamo/cinema-seat-reservation/internal/handler"
    "github.com/iliyamo/cinema-seat-reservation/internal/middleware"
    "github.com/labstack/echo/v4"
)

// RegisterOwnerReservations registers routes that allow owners to manage
// reservations.  All routes are mounted under /v1 and require a
// JWT token as well as the OWNER role.  The provided handler
// supplies the business logic for listing, retrieving and deleting
// reservations.
func RegisterOwnerReservations(e *echo.Echo, h *handler.OwnerReservationHandler, jwtSecret string) {
    g := e.Group(
        "/v1",
        middleware.JWTAuth(jwtSecret),
        middleware.RequireRole("OWNER"),
    )
    // List all reservations for a specific show
    g.GET("/shows/:id/reservations", h.ListShowReservations)
    // Retrieve a single reservation (owner perspective)
    g.GET("/owner/reservations/:id", h.GetOwnerReservation)
    // Cancel a reservation before the show starts (owner override)
    g.DELETE("/owner/reservations/:id", h.DeleteOwnerReservation)
}