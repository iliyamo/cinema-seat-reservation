package handler

import (
    "context"                      // context for redis operations
    "database/sql"                 // for sentinel errors returned from repository
    "encoding/json"                // encoding/decoding JSON for Redis values
    "errors"                       // for errors.Is comparisons
    "fmt"                          // for string formatting of Redis keys
    "net/http"                     // HTTP status codes
    "strconv"                      // parsing path parameters
    "time"                         // working with timestamps

    "github.com/redis/go-redis/v9" // Redis client (v9)

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

    // RedisClient is an optional client used to cache seat hold
    // information and perform rate limiting.  If nil, the handler
    // gracefully falls back to database only logic.  This client is
    // injected at application startup.  See internal/config/redis.go
    // for details on how the client is constructed.
    RedisClient *redis.Client

    // RateLimitMaxTokens defines the maximum number of tokens in the
    // token bucket used to rate‑limit hold requests.  Only relevant
    // when RedisClient is non‑nil.
    RateLimitMaxTokens int
    // RateLimitRefillRate defines how many tokens are replenished per
    // second.  A value of 1 means one token is added each second.  The
    // maximum number of tokens is limited by RateLimitMaxTokens.
    RateLimitRefillRate float64
    // RateLimitTTL is the expiration duration applied to the rate
    // limiting key.  It should exceed the time needed to fully
    // replenish the bucket.  When the key expires, a fresh bucket is
    // created on the next request.
    RateLimitTTL time.Duration
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
        // RedisClient and rate limit parameters are set externally after
        // construction.  They default to nil and zero values here.
        RedisClient:      nil,
        RateLimitMaxTokens: 0,
        RateLimitRefillRate: 0,
        RateLimitTTL:       0,
	}
}

// allowHold implements a token bucket rate limiter using Redis.  Each
// user has a dedicated key "rate:<user_id>" storing the number of
// available tokens and the timestamp of the last refill.  When a
// request arrives, tokens are replenished based on the time elapsed
// since the last refill (up to the configured maximum).  If at least
// one token is available it is consumed and the request is allowed.
// Otherwise the request is rejected.  The key TTL is refreshed on
// each update to allow the bucket to expire after a period of
// inactivity.  If RedisClient is nil or a Redis operation fails,
// allowHold returns true to fallback to unlimited access.  The
// implementation uses WATCH/MULTI to ensure atomic updates under
// concurrent access.
func (h *CustomerHandler) allowHold(ctx context.Context, userID uint64) (bool, error) {
    // If no Redis client is configured, always allow.
    if h.RedisClient == nil {
        return true, nil
    }
    // Ensure rate limit parameters are sane; if not configured, allow.
    if h.RateLimitMaxTokens <= 0 || h.RateLimitRefillRate <= 0 {
        return true, nil
    }
    key := fmt.Sprintf("rate:%d", userID)
    // We'll retry if a concurrent update causes the transaction to fail.
    for {
        err := h.RedisClient.Watch(ctx, func(tx *redis.Tx) error {
            // Attempt to get the current state.
            data, err := tx.Get(ctx, key).Bytes()
            // Default values for a new bucket.
            tokens := float64(h.RateLimitMaxTokens)
            lastRefill := float64(time.Now().Unix())
            if err == nil {
                // Attempt to parse existing JSON value.
                var state struct {
                    Tokens     float64 `json:"tokens"`
                    LastRefill int64   `json:"last_refill"`
                }
                if uErr := json.Unmarshal(data, &state); uErr == nil {
                    tokens = state.Tokens
                    lastRefill = float64(state.LastRefill)
                    // Refill tokens based on elapsed seconds.
                    now := float64(time.Now().Unix())
                    // Number of seconds since the last refill.
                    diff := now - lastRefill
                    if diff > 0 {
                        tokens += diff * h.RateLimitRefillRate
                        if tokens > float64(h.RateLimitMaxTokens) {
                            tokens = float64(h.RateLimitMaxTokens)
                        }
                        lastRefill = now
                    }
                }
                // If JSON unmarshal fails, treat as new bucket.
            } else if err != redis.Nil {
                // Unexpected error retrieving the key; fallback to allow.
                return err
            }
            // Check if we have a token to consume.
            if tokens < 1.0 {
                // Reject by returning a sentinel error that will break out of the watch.
                return redis.TxFailedErr
            }
            // Consume one token.
            tokens -= 1.0
            // Marshal the updated state back to JSON.
            newState, _ := json.Marshal(struct {
                Tokens     float64 `json:"tokens"`
                LastRefill int64   `json:"last_refill"`
            }{tokens, int64(lastRefill)})
            // Execute the transaction to update the key with the new state and TTL.
            _, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
                pipe.Set(ctx, key, newState, h.RateLimitTTL)
                return nil
            })
            return err
        }, key)
        if err == nil {
            // Transaction committed successfully; request is allowed.
            return true, nil
        }
        if errors.Is(err, redis.TxFailedErr) {
            // Token was not available; reject request.
            return false, nil
        }
        // On other errors, fallback to allowing the request.
        return true, nil
    }
}

// HoldSeats handles POST /v1/shows/:id/hold.  It allows a customer to
// temporarily hold one or more seats for five minutes.  To prevent
// race conditions when multiple users attempt to hold the same seat
// concurrently, this handler uses row‑level locks on show_seats via
// SELECT ... FOR UPDATE.  Each requested seat is locked and its
// current status checked; only seats with status FREE and no active
// seat_holds are holdable.  If a seat is RESERVED or already HELD,
// the handler rejects the request and returns the unavailable seat IDs.
// On success it inserts seat_holds records, updates show_seats.status
// to HELD and commits the transaction, releasing the locks.
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
    // ------------------------------------------------------------------
    // Use row‑level locks to safely check and hold seats.  Without locking
    // concurrent requests could both see a seat as FREE and then both
    // update it to HELD, resulting in double booking.  SELECT … FOR UPDATE
    // locks the show_seats row until the transaction commits.
    // We'll build two lists: holdable (available seats) and unavailable.
    unavailable := make([]uint64, 0)
    holdable := make([]uint64, 0, len(unique))
    for _, sid := range unique {
        // Acquire lock on the show_seats row for this seat.  This lock
        // prevents other transactions from reading or updating the row
        // until we decide whether it's free.  If the row is missing this
        // scan will return sql.ErrNoRows which we treat as unavailable.
        var seatStatus string
        err := tx.QueryRowContext(ctx,
            `SELECT status FROM show_seats WHERE show_id = ? AND seat_id = ? FOR UPDATE`,
            showID, sid,
        ).Scan(&seatStatus)
        if err != nil {
            // If the seat does not exist, treat it as unavailable
            if errors.Is(err, sql.ErrNoRows) {
                unavailable = append(unavailable, sid)
                continue
            }
            return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to lock seat"})
        }
        // Only seats with status FREE can be held.  RESERVED or HELD
        // seats are considered unavailable.  Using row‑level lock ensures
        // the status cannot change between this check and the update.
        if seatStatus != "FREE" {
            unavailable = append(unavailable, sid)
            continue
        }
        // Check if there is an active hold on this seat by any user.
        // Even if the show_seats.status is FREE, there may be an
        // unexpired seat_hold record.  We do not append FOR UPDATE
        // here because we already hold a lock on show_seats; counting
        // seat_holds does not require locking rows as we won't update
        // seat_holds until later.
        var holdCount int
        if err := tx.QueryRowContext(ctx,
            `SELECT COUNT(*) FROM seat_holds WHERE show_id = ? AND seat_id = ? AND expires_at > UTC_TIMESTAMP()`,
            showID, sid,
        ).Scan(&holdCount); err != nil {
            return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to check active holds"})
        }
        if holdCount > 0 {
            unavailable = append(unavailable, sid)
            continue
        }
        // Seat is free and not held; mark as holdable.  We keep the
        // row lock until the transaction commits to prevent others from
        // grabbing it concurrently.
        holdable = append(holdable, sid)
    }
    // If any seats are unavailable, abort the operation and return
    // them to the client.  The unavailable slice lists seats that are
    // either already HELD/RESERVED or missing.  We do not commit the
    // transaction in this case; the deferred rollback will release locks.
    if len(unavailable) > 0 {
        return c.JSON(http.StatusBadRequest, echo.Map{
            "error":       "some seats are unavailable",
            "unavailable": unavailable,
        })
    }
    // At this point we have locked all requested seats and verified
    // they are free.  Generate hold records with a 5 minute expiration.
    expiresAt := time.Now().UTC().Add(5 * time.Minute)
    holds, err := repository.GenerateHoldRecords(userID, showID, holdable, expiresAt)
    if err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to generate hold tokens"})
    }
    // Insert seat_holds rows.  This does not conflict with the locked
    // show_seats rows because we do not lock seat_holds when reading.
    if err := h.SeatHoldRepo.CreateMultipleTx(ctx, tx, holds); err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to create holds"})
    }
    // Update show_seats.status to HELD for each seat.  Because we still
    // hold the row locks from the earlier SELECT ... FOR UPDATE, this
    // update cannot conflict with another transaction.  The status and
    // version columns are updated atomically.
    if err := h.ShowSeatRepo.BulkUpdateStatusTx(ctx, tx, showID, holdable, "HELD"); err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to update seat status"})
    }
    // Commit the transaction.  This releases all row locks and makes
    // the holds visible to other transactions.
    if err := tx.Commit(); err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to commit transaction"})
    }
    committed = true
    // After the transaction commits, cache hold information in Redis so
    // that seat status can be determined without querying the
    // database.  Each seat receives a key with TTL equal to the hold
    // duration.  If RedisClient is nil or an error occurs, we ignore
    // the error and rely solely on the database status.  The
    // information stored includes the user ID and expiration time.
    if h.RedisClient != nil && len(holdable) > 0 {
        ttl := time.Until(expiresAt)
        for _, sid := range holdable {
            key := fmt.Sprintf("hold:%d:%d", showID, sid)
            val, _ := json.Marshal(struct {
                UserID    uint64 `json:"user_id"`
                ExpiresAt string `json:"expires_at"`
            }{userID, expiresAt.Format(time.RFC3339)})
            _ = h.RedisClient.Set(ctx, key, val, ttl).Err()
        }
    }
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
    // Remove cached hold keys from Redis now that the holds are released.
    if h.RedisClient != nil && len(seatIDs) > 0 {
        for _, sid := range seatIDs {
            key := fmt.Sprintf("hold:%d:%d", showID, sid)
            _ = h.RedisClient.Del(ctx, key).Err()
        }
    }
    return c.JSON(http.StatusOK, echo.Map{
        "released": len(seatIDs),
    })
}

// ConfirmSeats (also mapped to POST /v1/shows/:id/reserve) finalises
// previously held seats into a confirmed reservation.  To prevent
// race conditions with concurrent reservations, it acquires row‑level
// locks on each selected show_seats row via SELECT ... FOR UPDATE.
// This ensures that the seat remains HELD by the current user until the
// transaction commits.  The handler verifies that the seat status is
// HELD and that there is an active seat_hold for the user; if not it
// aborts.  After validation it creates a reservation and associated
// reservation_seats, updates show_seats.status to RESERVED and
// deletes the seat_holds.  The locks are released upon commit.
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
    // load active holds for user + show.  This fetches all seat_holds
    // belonging to the current user that have not expired.  We will
    // validate each hold individually under row‑level locks below.
    holds, err := h.SeatHoldRepo.ActiveHoldsByUserAndShowTx(ctx, tx, userID, showID)
    if err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to load holds"})
    }
    if len(holds) == 0 {
        return c.JSON(http.StatusBadRequest, echo.Map{"error": "no active holds for this show"})
    }
    // Build a set of held seat IDs for quick lookup and preserve order.
    seatIDs := make([]uint64, 0, len(holds))
    heldByUser := make(map[uint64]struct{})
    for _, hld := range holds {
        seatIDs = append(seatIDs, hld.SeatID)
        heldByUser[hld.SeatID] = struct{}{}
    }
    // Use row‑level locks to ensure that each seat is still HELD by this
    // user and has not been reserved or held by someone else in the
    // meantime.  Without locking, concurrent confirmations could both
    // see the seat as HELD and reserve it twice.  We track any seats
    // failing validation in unavailable.
    unavailable := make([]uint64, 0)
    for _, sid := range seatIDs {
        // Lock the show_seats row for this seat.  This prevents status
        // changes until we commit.  If the row is missing, treat as
        // unavailable.
        var seatStatus string
        if err := tx.QueryRowContext(ctx,
            `SELECT status FROM show_seats WHERE show_id = ? AND seat_id = ? FOR UPDATE`,
            showID, sid,
        ).Scan(&seatStatus); err != nil {
            if errors.Is(err, sql.ErrNoRows) {
                unavailable = append(unavailable, sid)
                continue
            }
            return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to lock seat"})
        }
        // Seat must currently be HELD.  If it is FREE or RESERVED, the
        // hold is invalid or has been overtaken by another transaction.
        if seatStatus != "HELD" {
            unavailable = append(unavailable, sid)
            continue
        }
        // Verify the seat hold record still belongs to the user.  We
        // query seat_holds to ensure there is exactly one active hold by
        // this user for this seat.  Without this check, a seat could be
        // held by another user but still have status HELD.
        var cnt int
        if err := tx.QueryRowContext(ctx,
            `SELECT COUNT(*) FROM seat_holds WHERE show_id = ? AND seat_id = ? AND user_id = ? AND expires_at > UTC_TIMESTAMP()`,
            showID, sid, userID,
        ).Scan(&cnt); err != nil {
            return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to verify seat hold"})
        }
        if cnt == 0 {
            unavailable = append(unavailable, sid)
            continue
        }
    }
    if len(unavailable) > 0 {
        // One or more seats cannot be confirmed.  Abort without
        // committing; rollback will release locks.  Return a 400 so
        // the client knows which seats failed.  Removing holds or
        // cleaning up is not performed here; clients may retry.
        return c.JSON(http.StatusBadRequest, echo.Map{
            "error":       "some seats cannot be confirmed",
            "unavailable": unavailable,
        })
    }
    // Compute total price from show_seats for the held seats.  We do
    // this after locking to ensure consistent pricing.  If any seat is
    // missing a price, return an error.  priceMap maps seat_id to price.
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
    // Insert the reservation record.  We set status to CONFIRMED as
    // holds are turned into a final reservation.  The ID is
    // auto‑generated by the database.
    resRec := &repository.ReservationRecord{
        UserID:           userID,
        ShowID:           showID,
        Status:           "CONFIRMED",
        TotalAmountCents: total,
    }
    if err := h.ReservationRepo.CreateTx(ctx, tx, resRec); err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to create reservation"})
    }
    // Prepare reservation_seats entries for each seat.  These map the
    // reservation to individual seats and their prices.
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
    // Update show_seats.status to RESERVED for all seats.  Because we
    // still hold row‑level locks, no other transaction can change the
    // status concurrently.  BulkUpdateStatusTx increments the version
    // and updates updated_at.
    if err := h.ShowSeatRepo.BulkUpdateStatusTx(ctx, tx, showID, seatIDs, "RESERVED"); err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to update seat status"})
    }
    // Remove seat_holds for this user and show.  This frees the
    // seat_holds rows and prevents duplicate confirmations.  We ignore
    // the returned list of seat IDs here since we already know them.
    if _, err := h.SeatHoldRepo.DeleteByUserAndShowTx(ctx, tx, userID, showID); err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to delete holds"})
    }
    // Commit the transaction to persist all changes and release locks.
    if err := tx.Commit(); err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to commit transaction"})
    }
    committed = true
    // Delete cached hold keys in Redis now that the seats are confirmed.
    if h.RedisClient != nil {
        for _, sid := range seatIDs {
            key := fmt.Sprintf("hold:%d:%d", showID, sid)
            _ = h.RedisClient.Del(ctx, key).Err()
        }
    }
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

    // Apply rate limiting before starting a DB transaction.  This uses a
    // token bucket implemented in Redis to prevent abuse of the hold
    // endpoint.  If the user has exhausted their quota, respond with
    // HTTP 429.  Any error from allowHold is ignored and we proceed.
    if ok, errRate := h.allowHold(ctx, userID); errRate == nil {
        if !ok {
            return c.JSON(http.StatusTooManyRequests, echo.Map{"error": "too many requests"})
        }
    }
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
