package handler

// This file defines HTTP handlers for owners to manage reservations.  Owners
// can view and cancel reservations for shows that belong to their own halls.
// The handlers ensure that the requesting user has the OWNER role via
// middleware and that the reservation or show belongs to the owner.  All
// critical database operations are executed within transactions to maintain
// data integrity.

import (
    "database/sql" // for sentinel errors
    "errors"       // for errors.Is comparisons
    "net/http"
    "strconv"
    "time"

    "github.com/iliyamo/cinema-seat-reservation/internal/repository"
    "github.com/labstack/echo/v4"
)

// OwnerReservationHandler groups repositories needed to list, view and
// cancel reservations from the perspective of a hall owner.  It
// encapsulates access to reservations, shows and show_seats.  The
// ShowRepo's DB handle is used for starting transactions.
type OwnerReservationHandler struct {
    ReservationRepo *repository.ReservationRepo // access to reservations and their seats
    ShowRepo        *repository.ShowRepo        // access to shows for transaction and existence checks
    HallRepo        *repository.HallRepo        // access to halls (unused directly but kept for symmetry)
    ShowSeatRepo    *repository.ShowSeatRepo    // access to show_seats for freeing seats on cancellation
}

// NewOwnerReservationHandler constructs an OwnerReservationHandler with
// the required repositories.  All dependencies must be non-nil.
func NewOwnerReservationHandler(resRepo *repository.ReservationRepo, showRepo *repository.ShowRepo, hallRepo *repository.HallRepo, showSeatRepo *repository.ShowSeatRepo) *OwnerReservationHandler {
    if resRepo == nil || showRepo == nil || showSeatRepo == nil {
        panic("nil repository passed to NewOwnerReservationHandler")
    }
    return &OwnerReservationHandler{
        ReservationRepo: resRepo,
        ShowRepo:        showRepo,
        HallRepo:        hallRepo,
        ShowSeatRepo:    showSeatRepo,
    }
}

// ListShowReservations handles GET /v1/shows/:id/reservations.  It
// returns all reservations for a show if the show belongs to the
// authenticated owner.  When the show is not owned by the caller,
// it returns HTTP 403.  An empty array is returned when no
// reservations exist.  The path parameter `id` must refer to an
// existing show.
func (h *OwnerReservationHandler) ListShowReservations(c echo.Context) error {
    ownerID, err := getUserID(c)
    if err != nil {
        return c.JSON(http.StatusUnauthorized, echo.Map{"error": "unauthorized"})
    }
    showID, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil || showID == 0 {
        return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid show id"})
    }
    ctx := c.Request().Context()
    details, err := h.ReservationRepo.ListByShowForOwner(ctx, showID, ownerID)
    if err != nil {
        // If the show does not exist, the repository will return sql.ErrNoRows.
        // Surface that as a 404 to the client.  A forbidden error indicates that
        // the show exists but belongs to a different owner.
        if errors.Is(err, sql.ErrNoRows) {
            return c.JSON(http.StatusNotFound, echo.Map{"error": "show not found"})
        }
        if errors.Is(err, repository.ErrForbidden) {
            return c.JSON(http.StatusForbidden, echo.Map{"error": "forbidden"})
        }
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to load reservations"})
    }
    // Always return a count and items.  When no reservations exist, details will
    // be an empty slice and count will be zero.
    return c.JSON(http.StatusOK, echo.Map{
        "items": details,
        "count": len(details),
    })
}

// GetOwnerReservation handles GET /v1/owner/reservations/:id.  It
// returns the details of a reservation when the underlying show is
// owned by the authenticated owner.  It returns HTTP 404 when the
// reservation does not exist and HTTP 403 when the owner does not
// own the reservation.  The path parameter `id` must be a valid
// reservation ID.
func (h *OwnerReservationHandler) GetOwnerReservation(c echo.Context) error {
    ownerID, err := getUserID(c)
    if err != nil {
        return c.JSON(http.StatusUnauthorized, echo.Map{"error": "unauthorized"})
    }
    resID, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil || resID == 0 {
        return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid reservation id"})
    }
    ctx := c.Request().Context()
    detail, err := h.ReservationRepo.GetByIDForOwner(ctx, resID, ownerID)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return c.JSON(http.StatusNotFound, echo.Map{"error": "reservation not found"})
        }
        if errors.Is(err, repository.ErrForbidden) {
            return c.JSON(http.StatusForbidden, echo.Map{"error": "forbidden"})
        }
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to fetch reservation"})
    }
    return c.JSON(http.StatusOK, echo.Map{
        "item": detail,
    })
}

// DeleteOwnerReservation handles DELETE /v1/owner/reservations/:id.  It
// cancels a reservation on behalf of an owner if the reservation's
// show belongs to the owner and has not started yet.  It returns
// HTTP 204 on success.  When the reservation does not exist it
// responds with 404.  When ownership is violated it responds with
// 403.  When the show has already started it responds with 409.
// Operations are performed within a single transaction to ensure
// atomicity.
func (h *OwnerReservationHandler) DeleteOwnerReservation(c echo.Context) error {
    ownerID, err := getUserID(c)
    if err != nil {
        return c.JSON(http.StatusUnauthorized, echo.Map{"error": "unauthorized"})
    }
    resID, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil || resID == 0 {
        return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid reservation id"})
    }
    ctx := c.Request().Context()
    tx, err := h.ShowRepo.DB().BeginTx(ctx, nil)
    if err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to start transaction"})
    }
    committed := false
    defer func() {
        if !committed {
            _ = tx.Rollback()
        }
    }()
    showID, startTime, seatIDs, err := h.ReservationRepo.GetInfoForOwnerTx(ctx, tx, resID, ownerID)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return c.JSON(http.StatusNotFound, echo.Map{"error": "reservation not found"})
        }
        if errors.Is(err, repository.ErrForbidden) {
            return c.JSON(http.StatusForbidden, echo.Map{"error": "forbidden"})
        }
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to load reservation info"})
    }
    if !startTime.After(time.Now().UTC()) {
        return c.JSON(http.StatusConflict, echo.Map{"error": "show already started"})
    }
    // Delete reservation (cascade deletes its reservation_seats)
    const del = `DELETE FROM reservations WHERE id = ?`
    if _, err := tx.ExecContext(ctx, del, resID); err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to delete reservation"})
    }
    // Free seats
    if len(seatIDs) > 0 {
        if err := h.ShowSeatRepo.BulkUpdateStatusTx(ctx, tx, showID, seatIDs, "FREE"); err != nil {
            return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to update seat status"})
        }
    }
    if err := tx.Commit(); err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to commit transaction"})
    }
    committed = true
    return c.NoContent(http.StatusNoContent)
}