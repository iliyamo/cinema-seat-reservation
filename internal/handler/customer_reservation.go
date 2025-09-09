package handler

import (
    "database/sql"   // for sentinel errors returned from repository
    "errors"         // for errors.Is comparisons
    "net/http"       // HTTP status codes
    "strconv"        // parsing path parameters
    "time"           // working with timestamps

    "github.com/iliyamo/cinema-seat-reservation/internal/repository" // repository layer
    "github.com/labstack/echo/v4"                                    // Echo web framework
)

// CustomerHandler groups repositories required to perform seat holds,
// confirmations and reservation listing on behalf of customers.  All
// methods assume that JWT authentication and role validation has
// already been performed by middleware.  Methods may return 401
// Unauthorized if the user ID cannot be extracted from the context.
// Each method runs critical DB operations inside a transaction to
// guarantee atomicity.
type CustomerHandler struct {
	SeatRepo        *repository.SeatRepo        // access to seats (unused directly but retained for future)
	ShowRepo        *repository.ShowRepo        // access to shows
	ShowSeatRepo    *repository.ShowSeatRepo    // access to show_seats for status updates and price queries
	SeatHoldRepo    *repository.SeatHoldRepo    // access to seat_holds for creating and deleting holds
	ReservationRepo *repository.ReservationRepo // access to reservations and reservation_seats
	HallRepo        *repository.HallRepo        // access to halls for potential lookups
	CinemaRepo      *repository.CinemaRepo      // access to cinemas for reservation listing
}

// NewCustomerHandler constructs a new CustomerHandler with the provided
// repositories.  All dependencies must be non-nil.
func NewCustomerHandler(seatRepo *repository.SeatRepo, showRepo *repository.ShowRepo, showSeatRepo *repository.ShowSeatRepo, seatHoldRepo *repository.SeatHoldRepo, reservationRepo *repository.ReservationRepo, hallRepo *repository.HallRepo, cinemaRepo *repository.CinemaRepo) *CustomerHandler {
	if seatRepo == nil || showRepo == nil || showSeatRepo == nil || seatHoldRepo == nil || reservationRepo == nil {
		panic("nil repository passed to NewCustomerHandler")
	}
	return &CustomerHandler{
		SeatRepo:        seatRepo,
		ShowRepo:        showRepo,
		ShowSeatRepo:    showSeatRepo,
		SeatHoldRepo:    seatHoldRepo,
		ReservationRepo: reservationRepo,
		HallRepo:        hallRepo,
		CinemaRepo:      cinemaRepo,
	}
}

// HoldSeats handles POST /v1/shows/:id/hold.  It allows a customer to
// temporarily hold one or more seats for five minutes.  The request body
// must contain a JSON object with a "seat_ids" array of positive
// integers.  It returns a 201 Created response with the expiration
// timestamp when successful.  If any requested seat is not available
// (already reserved or held by another user) it returns 400 with an
// error message and the list of unavailable seat IDs.
func (h *CustomerHandler) HoldSeats(c echo.Context) error {
	userID, err := getUserID(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "unauthorized"})
	}
	showID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || showID == 0 {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid show id"})
	}
	// ensure show exists
	if _, err := h.ShowRepo.GetByID(c.Request().Context(), showID); err != nil {
		if err == repository.ErrShowNotFound {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "show not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "database error"})
	}
	// bind request body
	var body struct {
		SeatIDs []uint64 `json:"seat_ids"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request body"})
	}
	if len(body.SeatIDs) == 0 {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "seat_ids is required"})
	}
	// deduplicate seat IDs to avoid duplicate holds
	unique := make([]uint64, 0, len(body.SeatIDs))
	seen := make(map[uint64]struct{})
	for _, id := range body.SeatIDs {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "no valid seat IDs provided"})
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
	// expire any holds that have passed expiration before checking availability
	if h.SeatHoldRepo != nil {
		if expired, errExp := h.SeatHoldRepo.ExpireHoldsTx(ctx, tx, showID); errExp == nil {
			if len(expired) > 0 {
				if errUp := h.ShowSeatRepo.BulkUpdateStatusTx(ctx, tx, showID, expired, "FREE"); errUp != nil {
					return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to cleanup expired holds"})
				}
			}
		} else {
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to cleanup expired holds"})
		}
	}
	// filter holdable seats within transaction
	holdable, err := h.ShowSeatRepo.FilterHoldableSeatsTx(ctx, tx, showID, unique)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to check seat availability"})
	}
	if len(holdable) != len(unique) {
		// find unavailable seats by comparing
		unavailable := make([]uint64, 0, len(unique)-len(holdable))
		allowed := make(map[uint64]struct{})
		for _, id := range holdable {
			allowed[id] = struct{}{}
		}
		for _, id := range unique {
			if _, ok := allowed[id]; !ok {
				unavailable = append(unavailable, id)
			}
		}
		return c.JSON(http.StatusBadRequest, echo.Map{
			"error":       "some seats are unavailable",
			"unavailable": unavailable,
		})
	}
	// compute expiration 5 minutes from now
	expiresAt := time.Now().UTC().Add(5 * time.Minute)
	// generate hold records with random tokens
	holds, err := repository.GenerateHoldRecords(userID, showID, holdable, expiresAt)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to generate hold tokens"})
	}
	if err := h.SeatHoldRepo.CreateMultipleTx(ctx, tx, holds); err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to create holds"})
	}
	// update show_seats status to HELD
	if err := h.ShowSeatRepo.BulkUpdateStatusTx(ctx, tx, showID, holdable, "HELD"); err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to update seat status"})
	}
	if err := tx.Commit(); err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to commit transaction"})
	}
	committed = true
	return c.JSON(http.StatusCreated, echo.Map{
		"expires_at": expiresAt.Format(time.RFC3339),
		"seat_ids":   holdable,
	})
}

// ReleaseHolds handles DELETE /v1/shows/:id/hold.  It releases all holds for
// the current user on the specified show.  Seats that were held are
// transitioned back to FREE.  Returns 200 OK with the number of seats
// released.
func (h *CustomerHandler) ReleaseHolds(c echo.Context) error {
	userID, err := getUserID(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "unauthorized"})
	}
	showID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || showID == 0 {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid show id"})
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
	seatIDs, err := h.SeatHoldRepo.DeleteByUserAndShowTx(ctx, tx, userID, showID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to release holds"})
	}
	// update seats back to FREE
	if len(seatIDs) > 0 {
		if err := h.ShowSeatRepo.BulkUpdateStatusTx(ctx, tx, showID, seatIDs, "FREE"); err != nil {
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to update seat status"})
		}
	}
	if err := tx.Commit(); err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to commit transaction"})
	}
	committed = true
	return c.JSON(http.StatusOK, echo.Map{
		"released": len(seatIDs),
	})
}

// ConfirmSeats handles POST /v1/shows/:id/confirm.  It finalises a hold and
// creates a reservation.  The handler verifies that the user still has
// active holds on seats for the show and that the holds have not
// expired.  It then creates a reservation record, reservation_seats
// entries, updates the show seat statuses to RESERVED and removes the
// holds.  Returns 201 Created with the reservation ID and total price.
func (h *CustomerHandler) ConfirmSeats(c echo.Context) error {
	userID, err := getUserID(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "unauthorized"})
	}
	showID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || showID == 0 {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid show id"})
	}
	// ensure show exists
	if _, err := h.ShowRepo.GetByID(c.Request().Context(), showID); err != nil {
		if err == repository.ErrShowNotFound {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "show not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "database error"})
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
	// expire any holds that have passed expiration before confirming
	if h.SeatHoldRepo != nil {
		if expired, errExp := h.SeatHoldRepo.ExpireHoldsTx(ctx, tx, showID); errExp == nil {
			if len(expired) > 0 {
				if errUp := h.ShowSeatRepo.BulkUpdateStatusTx(ctx, tx, showID, expired, "FREE"); errUp != nil {
					return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to cleanup expired holds"})
				}
			}
		} else {
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to cleanup expired holds"})
		}
	}
	// load active holds for user + show
	holds, err := h.SeatHoldRepo.ActiveHoldsByUserAndShowTx(ctx, tx, userID, showID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to load holds"})
	}
	if len(holds) == 0 {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "no active holds for this show"})
	}
	// extract seat IDs from holds
	seatIDs := make([]uint64, 0, len(holds))
	for _, hld := range holds {
		seatIDs = append(seatIDs, hld.SeatID)
	}
	// compute total price from show_seats
	priceMap, err := h.ShowSeatRepo.GetPricesBySeatIDsTx(ctx, tx, showID, seatIDs)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to fetch seat prices"})
	}
	total := uint32(0)
	for _, sid := range seatIDs {
		if p, ok := priceMap[sid]; ok {
			total += p
		} else {
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "price not found for seat"})
		}
	}
	// create reservation record
	resRec := &repository.ReservationRecord{
		UserID:           userID,
		ShowID:           showID,
		Status:           "CONFIRMED",
		TotalAmountCents: total,
	}
	if err := h.ReservationRepo.CreateTx(ctx, tx, resRec); err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to create reservation"})
	}
	// create reservation_seats entries
	seats := make([]repository.ReservationSeatRecord, 0, len(seatIDs))
	for _, sid := range seatIDs {
		seats = append(seats, repository.ReservationSeatRecord{
			ReservationID: resRec.ID,
			ShowID:        showID,
			SeatID:        sid,
			PriceCents:    priceMap[sid],
		})
	}
	if err := h.ReservationRepo.CreateSeatsBulkTx(ctx, tx, seats); err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to create reservation seats"})
	}
	// update show seats to RESERVED
	if err := h.ShowSeatRepo.BulkUpdateStatusTx(ctx, tx, showID, seatIDs, "RESERVED"); err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to update seat status"})
	}
	// delete holds for this user and show
	if _, err := h.SeatHoldRepo.DeleteByUserAndShowTx(ctx, tx, userID, showID); err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to delete holds"})
	}
	if err := tx.Commit(); err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to commit transaction"})
	}
	committed = true
	return c.JSON(http.StatusCreated, echo.Map{
		"reservation_id":     resRec.ID,
		"total_amount_cents": total,
	})
}

// ListReservations handles GET /v1/my-reservations.  It returns all
// reservations created by the current user along with show, hall,
// cinema and seat details.  When no reservations exist, it returns an
// empty array.  The response structure matches ReservationDetail
// defined in the repository layer.
func (h *CustomerHandler) ListReservations(c echo.Context) error {
	userID, err := getUserID(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "unauthorized"})
	}
	ctx := c.Request().Context()
	details, err := h.ReservationRepo.ListByUser(ctx, userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to load reservations"})
	}
	return c.JSON(http.StatusOK, echo.Map{
		"items": details,
	})
}

// GetReservation handles GET /v1/reservations/:id.  It returns the
// details of a single reservation for the authenticated user.  When
// the reservation does not exist, it responds with 404.  When the
// reservation belongs to a different user, it responds with 403.  Any
// unexpected error results in a 500 response.
func (h *CustomerHandler) GetReservation(c echo.Context) error {
    userID, err := getUserID(c)
    if err != nil {
        return c.JSON(http.StatusUnauthorized, echo.Map{"error": "unauthorized"})
    }
    resID, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil || resID == 0 {
        return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid reservation id"})
    }
    ctx := c.Request().Context()
    detail, err := h.ReservationRepo.GetByIDForUser(ctx, resID, userID)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            // reservation not found or not owned by user (ownership enforced in repo)
            return c.JSON(http.StatusNotFound, echo.Map{"error": "reservation not found"})
        }
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to fetch reservation"})
    }
    return c.JSON(http.StatusOK, echo.Map{
        "item": detail,
    })
}

// DeleteReservation handles DELETE /v1/reservations/:id.  It cancels a
// reservation belonging to the current user if the associated show has
// not yet started.  It returns 204 on success, 404 when the
// reservation does not exist, 403 when the reservation belongs to
// another user, and 409 when the show has already started.  All
// operations are executed within a transaction.
func (h *CustomerHandler) DeleteReservation(c echo.Context) error {
    userID, err := getUserID(c)
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
    showID, startTime, seatIDs, err := h.ReservationRepo.GetInfoForUserTx(ctx, tx, resID, userID)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return c.JSON(http.StatusNotFound, echo.Map{"error": "reservation not found"})
        }
        if errors.Is(err, repository.ErrForbidden) {
            return c.JSON(http.StatusForbidden, echo.Map{"error": "forbidden"})
        }
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to load reservation info"})
    }
    // Check if the show has already started; if so, return conflict
    if !startTime.After(time.Now().UTC()) {
        return c.JSON(http.StatusConflict, echo.Map{"error": "show already started"})
    }
    // Delete the reservation; cascade deletes reservation_seats due to FK
    const del = `DELETE FROM reservations WHERE id = ?`
    if _, err := tx.ExecContext(ctx, del, resID); err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to delete reservation"})
    }
    // Return seats to FREE status
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
