package handler // handler package contains owner-specific show handlers

import (
    "database/sql"                                           // sql is needed for sentinel errors during show updates
    "net/http"                                               // http defines status codes
    "strconv"                                                // strconv converts path params to integers
    "strings"                                                // strings helps with trimming whitespace
    "time"                                                   // time is used for parsing and formatting timestamps

    "github.com/iliyamo/cinema-seat-reservation/internal/repository" // repository defines data models
    "github.com/labstack/echo/v4"                                   // echo provides the web context and JSON helpers
)

// CreateShow handles POST /v1/shows and schedules a new show in a hall.  It creates show seats for all hall seats.
func (h *OwnerHandler) CreateShow(c echo.Context) error { // begin CreateShow handler
    ownerID, err := getUserID(c) // extract user ID from context
    if err != nil { // unauthorized when user ID is invalid
        return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // respond unauthorized
    }
    var body struct { // struct to bind JSON request body
        HallID         uint64  `json:"hall_id"`         // ID of the hall where the show will take place
        Title          string  `json:"title"`           // legacy field for movie title
        MovieTitle     string  `json:"movie_title"`     // preferred field for movie title
        StartsAt       string  `json:"starts_at"`       // ISO start time
        EndsAt         string  `json:"ends_at"`         // ISO end time
        BasePriceCents *uint32 `json:"base_price_cents"` // optional base price for seats
    }
    if err := c.Bind(&body); err != nil { // bind incoming JSON
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"}) // respond bad request on binding failure
    }
    if body.HallID == 0 { // hall ID must be provided
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "hall_id is required"}) // respond missing hall id
    }
    title := strings.TrimSpace(body.MovieTitle) // prefer movie_title field
    if title == "" { // fallback to legacy title field
        title = strings.TrimSpace(body.Title) // use trimmed legacy title
    }
    if title == "" { // no title provided
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "movie_title is required"}) // respond missing title
    }
    startsAt := strings.TrimSpace(body.StartsAt) // trim the start time
    endsAt := strings.TrimSpace(body.EndsAt)     // trim the end time
    if startsAt == "" || endsAt == "" { // both times are required
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "starts_at and ends_at are required"}) // respond missing times
    }
    if _, err := h.HallRepo.GetByIDAndOwner(c.Request().Context(), body.HallID, ownerID); err != nil { // verify hall ownership
        if err == repository.ErrHallNotFound { // hall not found
            return c.JSON(http.StatusNotFound, map[string]string{"error": "hall not found"}) // respond not found
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to verify hall"}) // respond generic error
    }
    startTime, err := time.Parse(time.RFC3339, startsAt) // parse start time
    if err != nil { // invalid start time format
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid starts_at format"}) // respond invalid format
    }
    endTime, err := time.Parse(time.RFC3339, endsAt) // parse end time
    if err != nil { // invalid end time format
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid ends_at format"}) // respond invalid format
    }
    if !endTime.After(startTime) { // end must be after start
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "ends_at must be after starts_at"}) // respond invalid times
    }
    var price uint32 // default to zero price
    if body.BasePriceCents != nil { // base price provided
        price = *body.BasePriceCents // assign provided base price
    }
    // Before creating the show, ensure there is no overlapping show scheduled in the same hall.
    // Compose the start and end strings in the DB-friendly format.
    startStr := startTime.Format("2006-01-02 15:04:05")
    endStr := endTime.Format("2006-01-02 15:04:05")
    // Query for overlapping shows in this hall.  We do not need to filter by owner
    // because we already verified hall ownership above.
    overlaps, err := h.ShowRepo.FindOverlapping(c.Request().Context(), body.HallID, startStr, endStr)
    if err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to check existing shows"})
    }
    if len(overlaps) > 0 { // if there is at least one conflicting show
        // Return a conflict response describing the overlapping show(s).
        return c.JSON(http.StatusConflict, map[string]any{
            "error":      "show time overlaps with existing show",        // human-readable message
            "overlaps":   overlaps,                                       // include conflicting shows in response
        })
    }
    // Build the show struct to insert.
    show := &repository.Show{
        HallID:         body.HallID,      // assign hall ID
        Title:          title,            // assign movie title
        StartsAt:       startStr,         // assign formatted start time
        EndsAt:         endStr,           // assign formatted end time
        BasePriceCents: price,            // base price for seats
    }
    // Create the show in the repository; this also populates ID and default fields.
    if err := h.ShowRepo.Create(c.Request().Context(), show); err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not create show"})
    }
    // Load seats for the hall to create show_seat entries.  If there are none
    // (possibly due to misconfiguration), return internal error.
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
    // Retrieve the fully populated show from DB (including status and timestamps).
    fresh, err := h.ShowRepo.GetByID(c.Request().Context(), show.ID)
    if err != nil {
        // fallback: return the partially populated show even if retrieval fails
        return c.JSON(http.StatusCreated, show)
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
func (h *OwnerHandler) UpdateShow(c echo.Context) error { // begin UpdateShow handler
    ownerID, err := getUserID(c) // authenticate caller
    if err != nil {
        return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
    }
    // Parse show ID from path
    id, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil {
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
    }
    // Load the existing show
    cur, err := h.ShowRepo.GetByID(c.Request().Context(), id)
    if err != nil {
        if err == repository.ErrShowNotFound {
            return c.JSON(http.StatusNotFound, map[string]string{"error": "show not found"})
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load show"})
    }
    // Verify ownership by ensuring the hall belongs to the owner
    if _, err := h.HallRepo.GetByIDAndOwner(c.Request().Context(), cur.HallID, ownerID); err != nil {
        if err == repository.ErrHallNotFound {
            return c.JSON(http.StatusNotFound, map[string]string{"error": "show not found"})
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to verify ownership"})
    }
    // Bind request body
    var body struct {
        Title          *string `json:"title"`            // legacy field for movie title
        MovieTitle     *string `json:"movie_title"`      // preferred field for movie title
        StartsAt       *string `json:"starts_at"`        // optional new start time RFC3339
        EndsAt         *string `json:"ends_at"`          // optional new end time RFC3339
        BasePriceCents *uint32 `json:"base_price_cents"` // optional new base price
        Status         *string `json:"status"`           // optional new status
    }
    if err := c.Bind(&body); err != nil {
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
    }
    // Determine new title; prefer movie_title then title
    title := cur.Title
    if body.MovieTitle != nil && strings.TrimSpace(*body.MovieTitle) != "" {
        title = strings.TrimSpace(*body.MovieTitle)
    } else if body.Title != nil && strings.TrimSpace(*body.Title) != "" {
        title = strings.TrimSpace(*body.Title)
    }
    // Parse start/end times; if provided, update them; otherwise keep current
    start := cur.StartsAt
    end := cur.EndsAt
    var startTime, endTime time.Time
    var parsedStart, parsedEnd bool
    if body.StartsAt != nil && strings.TrimSpace(*body.StartsAt) != "" {
        st := strings.TrimSpace(*body.StartsAt)
        t, err := time.Parse(time.RFC3339, st)
        if err != nil {
            return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid starts_at format"})
        }
        startTime = t
        start = t.Format("2006-01-02 15:04:05")
        parsedStart = true
    }
    if body.EndsAt != nil && strings.TrimSpace(*body.EndsAt) != "" {
        et := strings.TrimSpace(*body.EndsAt)
        t, err := time.Parse(time.RFC3339, et)
        if err != nil {
            return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid ends_at format"})
        }
        endTime = t
        end = t.Format("2006-01-02 15:04:05")
        parsedEnd = true
    }
    // If either start or end is parsed, ensure both are set and end is after start
    if parsedStart || parsedEnd {
        // If one of the times wasn't updated, parse the other from current string to compare
        if !parsedStart {
            var err error
            startTime, err = time.Parse("2006-01-02 15:04:05", cur.StartsAt)
            if err != nil {
                // fallback; should not happen if DB stored valid format
                return c.JSON(http.StatusInternalServerError, map[string]string{"error": "invalid stored starts_at"})
            }
        }
        if !parsedEnd {
            var err error
            endTime, err = time.Parse("2006-01-02 15:04:05", cur.EndsAt)
            if err != nil {
                return c.JSON(http.StatusInternalServerError, map[string]string{"error": "invalid stored ends_at"})
            }
        }
        if !endTime.After(startTime) {
            return c.JSON(http.StatusBadRequest, map[string]string{"error": "ends_at must be after starts_at"})
        }
        // Check for overlaps with other shows in the hall
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
    // Determine new base price
    price := cur.BasePriceCents
    if body.BasePriceCents != nil {
        price = *body.BasePriceCents
    }
    // Determine new status; allowed statuses: SCHEDULED, CANCELLED, FINISHED
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
    // If all values unchanged, return conflict
    if title == cur.Title && start == cur.StartsAt && end == cur.EndsAt && price == cur.BasePriceCents && status == cur.Status {
        return c.JSON(http.StatusConflict, map[string]string{"error": "show already has these parameters"})
    }
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