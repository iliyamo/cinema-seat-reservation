package handler // handler package contains owner-specific show handlers

import (
	"database/sql" // sql is needed for sentinel errors during show updates
	"errors"
	"net/http" // http defines status codes
	"strconv"  // strconv converts path params to integers
	"strings"  // strings helps with trimming whitespace
	"time"     // time is used for parsing and formatting timestamps

	"github.com/iliyamo/cinema-seat-reservation/internal/repository" // repository defines data models
	"github.com/labstack/echo/v4"                                    // echo provides the web context and JSON helpers
)

// CreateShow handles POST /v1/shows and schedules a new show in a hall.  It creates show seats for all hall seats.
func (h *OwnerHandler) CreateShow(c echo.Context) error { // begin CreateShow handler
	ownerID, err := getUserID(c) // extract user ID from context
	if err != nil {              // unauthorized when user ID is invalid
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // respond unauthorized
	}
	var body struct { // struct to bind JSON request body
		HallID         uint64  `json:"hall_id"`          // ID of the hall where the show will take place
		Title          string  `json:"title"`            // legacy field for movie title
		MovieTitle     string  `json:"movie_title"`      // preferred field for movie title
		StartsAt       string  `json:"starts_at"`        // ISO start time (RFC3339)
		EndsAt         string  `json:"ends_at"`          // ISO end time (RFC3339)
		BasePriceCents *uint32 `json:"base_price_cents"` // optional base price for seats
	}
	if err := c.Bind(&body); err != nil { // bind incoming JSON
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"}) // respond bad request on binding failure
	}
	if body.HallID == 0 { // hall ID must be provided
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "hall_id is required"}) // respond missing hall id
	}
	title := strings.TrimSpace(body.MovieTitle) // prefer movie_title field
	if title == "" {                            // fallback to legacy title field
		title = strings.TrimSpace(body.Title) // use trimmed legacy title
	}
	if title == "" { // no title provided
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "movie_title is required"}) // respond missing title
	}
	startsAt := strings.TrimSpace(body.StartsAt) // trim the start time
	endsAt := strings.TrimSpace(body.EndsAt)     // trim the end time
	if startsAt == "" || endsAt == "" {          // both times are required
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "starts_at and ends_at are required"}) // respond missing times
	}
	// verify hall ownership
	if _, err := h.HallRepo.GetByIDAndOwner(c.Request().Context(), body.HallID, ownerID); err != nil {
		if err == repository.ErrHallNotFound {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "hall not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to verify hall"})
	}

	// parse RFC3339 and normalize to UTC to match DB DATETIME storage
	startTime, err := time.Parse(time.RFC3339, startsAt)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid starts_at format"})
	}
	endTime, err := time.Parse(time.RFC3339, endsAt)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid ends_at format"})
	}
	if !endTime.After(startTime) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "ends_at must be after starts_at"})
	}

	var price uint32
	if body.BasePriceCents != nil {
		price = *body.BasePriceCents
	}

	// Convert to DB-friendly UTC string "YYYY-MM-DD HH:MM:SS"
	startStr := startTime.UTC().Format("2006-01-02 15:04:05")
	endStr := endTime.UTC().Format("2006-01-02 15:04:05")

	// Ensure no overlap in this hall
	overlaps, err := h.ShowRepo.FindOverlapping(c.Request().Context(), body.HallID, startStr, endStr)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to check existing shows"})
	}
	if len(overlaps) > 0 {
		return c.JSON(http.StatusConflict, map[string]any{
			"error":    "show time overlaps with existing show",
			"overlaps": overlaps,
		})
	}

	// Build new show
	show := &repository.Show{
		HallID:         body.HallID,
		Title:          title,
		StartsAt:       startStr,
		EndsAt:         endStr,
		BasePriceCents: price,
	}
	// Create show
	if err := h.ShowRepo.Create(c.Request().Context(), show); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not create show"})
	}

	// Create show seats for all seats in the hall
	seats, err := h.SeatRepo.GetByHall(c.Request().Context(), body.HallID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load seats"})
	}
	ss := make([]repository.ShowSeat, 0, len(seats))
	for _, seat := range seats {
		ss = append(ss, repository.ShowSeat{
			ShowID:     show.ID,
			SeatID:     seat.ID,
			Status:     "FREE",
			PriceCents: price,
			Version:    1,
		})
	}
	if err := h.ShowSeatRepo.CreateBulk(c.Request().Context(), ss); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create show seats"})
	}

	// Return the fully populated show row
	fresh, err := h.ShowRepo.GetByID(c.Request().Context(), show.ID)
	if err != nil {
		return c.JSON(http.StatusCreated, show) // fallback
	}
	return c.JSON(http.StatusCreated, fresh)
}

// ListShowsInHall handles GET /v1/halls/:hall_id/shows and returns all shows for a hall owned by the caller.
func (h *OwnerHandler) ListShowsInHall(c echo.Context) error { // begin ListShowsInHall
	ownerID, err := getUserID(c) // extract user ID from context
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // unauthorized when user ID invalid
	}
	// Parse hall_id path parameter.
	hallID, err := strconv.ParseUint(c.Param("hall_id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid hall_id"})
	}
	// Ensure the hall exists and belongs to the owner.  Use HallRepo for verification.
	if _, err := h.HallRepo.GetByIDAndOwner(c.Request().Context(), hallID, ownerID); err != nil {
		if err == repository.ErrHallNotFound {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "hall not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"})
	}
	// Fetch all shows for this hall and owner.
	shows, err := h.ShowRepo.ListByHallAndOwner(c.Request().Context(), hallID, ownerID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load shows"})
	}
	return c.JSON(http.StatusOK, map[string]any{"items": shows})
}

// UpdateShow handles PUT/PATCH /v1/shows/:id and updates a show.  It allows modifying
// the title, start/end times, base price and status while enforcing ownership and
// avoiding schedule conflicts.  When times are changed, it checks for overlaps.
func (h *OwnerHandler) UpdateShow(c echo.Context) error {
	ownerID, err := getUserID(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}

	cur, err := h.ShowRepo.GetByID(c.Request().Context(), id)
	if err != nil {
		if err == repository.ErrShowNotFound {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "show not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load show"})
	}

	// verify ownership by hall
	if _, err := h.HallRepo.GetByIDAndOwner(c.Request().Context(), cur.HallID, ownerID); err != nil {
		if err == repository.ErrHallNotFound {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "show not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to verify ownership"})
	}

	// optional inputs
	var body struct {
		Title          *string `json:"title"`
		MovieTitle     *string `json:"movie_title"`
		StartsAt       *string `json:"starts_at"` // RFC3339
		EndsAt         *string `json:"ends_at"`   // RFC3339
		BasePriceCents *uint32 `json:"base_price_cents"`
		Status         *string `json:"status"` // SCHEDULED|CANCELLED|FINISHED
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	// derive new values (default to current DB values)
	title := cur.Title
	if body.MovieTitle != nil && strings.TrimSpace(*body.MovieTitle) != "" {
		title = strings.TrimSpace(*body.MovieTitle)
	} else if body.Title != nil && strings.TrimSpace(*body.Title) != "" {
		title = strings.TrimSpace(*body.Title)
	}

	start := cur.StartsAt
	end := cur.EndsAt
	var startChanged, endChanged bool

	if body.StartsAt != nil && strings.TrimSpace(*body.StartsAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*body.StartsAt))
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid starts_at format (RFC3339 required)"})
		}
		start = t.UTC().Format("2006-01-02 15:04:05") // normalize to UTC
		startChanged = true
	}
	if body.EndsAt != nil && strings.TrimSpace(*body.EndsAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*body.EndsAt))
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid ends_at format (RFC3339 required)"})
		}
		end = t.UTC().Format("2006-01-02 15:04:05") // normalize to UTC
		endChanged = true
	}

	// validate & overlap check only if times changed
	if startChanged || endChanged {
		ts, err := time.Parse("2006-01-02 15:04:05", start)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid starts_at"})
		}
		te, err := time.Parse("2006-01-02 15:04:05", end)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid ends_at"})
		}
		if !te.After(ts) {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "ends_at must be after starts_at"})
		}
		overlaps, err := h.ShowRepo.FindOverlappingExcluding(c.Request().Context(), cur.HallID, cur.ID, start, end)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to check overlapping shows"})
		}
		if len(overlaps) > 0 {
			return c.JSON(http.StatusConflict, map[string]any{
				"error":    "show time overlaps with existing show",
				"overlaps": overlaps,
			})
		}
	}

	price := cur.BasePriceCents
	if body.BasePriceCents != nil {
		price = *body.BasePriceCents
	}

	status := cur.Status
	if body.Status != nil {
		s := strings.ToUpper(strings.TrimSpace(*body.Status))
		switch s {
		case "SCHEDULED", "CANCELLED", "FINISHED":
			status = s
		default:
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid status"})
		}
	}

	// 🔒 guard: if nothing changed, do not update
	if title == cur.Title && start == cur.StartsAt && end == cur.EndsAt && price == cur.BasePriceCents && status == cur.Status {
		return c.JSON(http.StatusConflict, map[string]string{"error": "no changes"})
	}

	// perform update (ownership enforced in SQL JOIN)
	upd := &repository.Show{
		ID:             cur.ID,
		HallID:         cur.HallID,
		Title:          title,
		StartsAt:       start,
		EndsAt:         end,
		BasePriceCents: price,
		Status:         status,
	}

	if err := h.ShowRepo.UpdateByIDAndOwner(c.Request().Context(), upd, ownerID); err != nil {
		if errors.Is(err, repository.ErrNoChange) {
			return c.JSON(http.StatusConflict, map[string]string{"error": "no changes"})
		}
		if err == sql.ErrNoRows {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "show not found"})
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "update failed"})
	}

	fresh, err := h.ShowRepo.GetByID(c.Request().Context(), cur.ID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load show"})
	}
	return c.JSON(http.StatusOK, fresh)
}
