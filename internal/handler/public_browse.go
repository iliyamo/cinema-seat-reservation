package handler

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iliyamo/cinema-seat-reservation/internal/repository"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

// PublicHandler aggregates repositories needed for unauthenticated browsing.
// It produces sanitized responses suitable for public consumption.
type PublicHandler struct {
	CinemaRepo *repository.CinemaRepo // provides access to cinema data
	HallRepo   *repository.HallRepo   // provides access to hall data
	ShowRepo   *repository.ShowRepo   // provides access to show data

	// SeatRepo gives access to seats for hall layout and seat status endpoints.  It
	// is optional and may be nil in older constructions; handlers that require
	// SeatRepo should check for nil and return an internal error if absent.
	SeatRepo *repository.SeatRepo
	// ShowSeatRepo gives access to show_seats for seat status computation.
	ShowSeatRepo *repository.ShowSeatRepo

	// SeatHoldRepo gives access to seat_holds for expiring holds prior to
	// computing seat status.  It may be nil in legacy constructions; when
	// non-nil it will be used to expire holds before listing seats.
	SeatHoldRepo *repository.SeatHoldRepo

	// RedisClient optionally provides access to a Redis server for
	// caching seat hold information.  When set, GetPublicShowSeats
	// will consult Redis for active holds before reading from the
	// database.  If nil, the handler relies solely on the database.
	RedisClient *redis.Client
}

// PublicCinema represents a cinema exposed via the public API. It contains
// only safe fields.
type PublicCinema struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

// PublicHall represents a hall exposed via the public API.
type PublicHall struct {
	ID       uint64  `json:"id"`
	Name     string  `json:"name"`
	SeatRows *uint32 `json:"seat_rows,omitempty"`
	SeatCols *uint32 `json:"seat_cols,omitempty"`
}

// PublicShow represents a show in list responses. Both start and end times are
// exposed as ISO 8601 strings. Pointers are used so that nil values result
// in omitted or null fields when times are not present or invalid. This
// avoids returning Go's zero time (0001-01-01T00:00:00Z) to clients.
type PublicShow struct {
	ID    uint64 `json:"id"`    // ID uniquely identifies the show
	Title string `json:"title"` // Title is the movie or event name
	// StartTime is the ISO 8601 formatted start time of the show. It is a pointer
	// so that null values can be encoded as JSON null. The omitempty directive is
	// omitted to ensure the field always appears in the response, even when nil.
	StartTime *string `json:"start_time"`
	// EndTime is the ISO 8601 formatted end time of the show. Like StartTime,
	// it is a pointer to allow null values when no end time is provided. The
	// absence of omitempty causes the field to appear with a null value when nil.
	EndTime *string `json:"end_time"`
}

// PublicShowDetail represents a single show with related cinema and hall names.
// StartTime and EndTime are ISO 8601 strings or null pointers. Only
// non-sensitive fields are returned.

// toISOTime tries multiple layouts to parse a timestamp string and returns an ISO 8601 (RFC3339) string pointer.
// It accepts typical MySQL DATETIME ("2006-01-02 15:04:05"), RFC3339 (with or without zone),
// and returns nil for empty/zero values. If parsing fails but value is non-empty, it returns the original string.
func toISOTime(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" || s == "0001-01-01 00:00:00" || s == "0000-00-00 00:00:00" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05 -07:00",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			iso := t.UTC().Format(time.RFC3339)
			return &iso
		}
	}
	// Best effort: if it's already a plausible date string but didn't parse with our layouts,
	// return it as-is instead of null so clients aren't left without data.
	v := s
	return &v
}

type PublicShowDetail struct {
	ID    uint64 `json:"id"`    // show identifier
	Title string `json:"title"` // movie or event title
	// StartTime is the ISO 8601 formatted start time or null. It is not
	// tagged with omitempty so it will always appear in the JSON output.
	StartTime *string `json:"start_time"`
	// EndTime is the ISO 8601 formatted end time or null.
	EndTime *string `json:"end_time"`
	// Cinema contains the minimal cinema info (id, name) if available.
	Cinema *PublicCinema `json:"cinema,omitempty"`
	// Hall contains the minimal hall info (id, name) if available.
	Hall *struct {
		ID   uint64 `json:"id"`   // hall identifier
		Name string `json:"name"` // hall name
	} `json:"hall,omitempty"`
}

// GetPublicCinemas returns a list of all cinemas accessible to unauthenticated users.
// Response JSON contains an "items" array of PublicCinema.
func (h *PublicHandler) GetPublicCinemas(c echo.Context) error {
	ctx := c.Request().Context()
	cinemas, err := h.CinemaRepo.ListAll(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "database error"})
	}
	out := make([]PublicCinema, 0, len(cinemas))
	for _, cin := range cinemas {
		out = append(out, PublicCinema{ID: cin.ID, Name: cin.Name})
	}
	return c.JSON(http.StatusOK, echo.Map{"items": out})
}

// GetPublicHallsByCinema lists halls of a cinema for unauthenticated users. It validates
// the cinema exists, then returns only non-sensitive fields.
func (h *PublicHandler) GetPublicHallsByCinema(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
	}
	// ensure cinema exists
	if _, err := h.CinemaRepo.GetByID(ctx, id); err != nil {
		if err == repository.ErrCinemaNotFound {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "cinema not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "database error"})
	}
	halls, err := h.HallRepo.ListByCinema(ctx, id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "database error"})
	}
	out := make([]PublicHall, 0, len(halls))
	for _, hall := range halls {
		var rowsPtr, colsPtr *uint32
		if hall.SeatRows.Valid {
			v := uint32(hall.SeatRows.Int32)
			rowsPtr = &v
		}
		if hall.SeatCols.Valid {
			v := uint32(hall.SeatCols.Int32)
			colsPtr = &v
		}
		out = append(out, PublicHall{ID: hall.ID, Name: hall.Name, SeatRows: rowsPtr, SeatCols: colsPtr})
	}
	return c.JSON(http.StatusOK, echo.Map{"items": out})
}

// GetPublicShowsByHall lists shows in a hall for unauthenticated users. It ensures the hall
// exists, then returns each show's ID, title and start time as a time.Time.
func (h *PublicHandler) GetPublicShowsByHall(c echo.Context) error {
	ctx := c.Request().Context()
	hallID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
	}
	// ensure hall exists
	if _, err := h.HallRepo.GetByID(ctx, hallID); err != nil {
		if err == repository.ErrHallNotFound {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "hall not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "database error"})
	}
	shows, err := h.ShowRepo.ListByHall(ctx, hallID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "database error"})
	}
	out := make([]PublicShow, 0, len(shows))
	for _, s := range shows {
		out = append(out, PublicShow{ID: s.ID, Title: s.Title, StartTime: toISOTime(s.StartsAt), EndTime: toISOTime(s.EndsAt)})
	}
	return c.JSON(http.StatusOK, echo.Map{"items": out})
}

// GetPublicShow returns details of a single show for unauthenticated users. It joins
// hall and cinema names by following foreign keys. Only non-sensitive fields are included.
func (h *PublicHandler) GetPublicShow(c echo.Context) error {
	ctx := c.Request().Context()
	showID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
	}
	s, err := h.ShowRepo.GetByID(ctx, showID)
	if err != nil {
		if err == repository.ErrShowNotFound {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "show not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "database error"})
	}
	resp := PublicShowDetail{ID: s.ID, Title: s.Title, StartTime: toISOTime(s.StartsAt), EndTime: toISOTime(s.EndsAt)}
	// load hall to get hall name and cinema ID
	if hall, err := h.HallRepo.GetByID(ctx, s.HallID); err == nil {
		resp.Hall = &struct {
			ID   uint64 `json:"id"`
			Name string `json:"name"`
		}{ID: hall.ID, Name: hall.Name}
		if hall.CinemaID != nil {
			if cin, err2 := h.CinemaRepo.GetByID(ctx, *hall.CinemaID); err2 == nil {
				resp.Cinema = &PublicCinema{ID: cin.ID, Name: cin.Name}
			}
		}
	}
	return c.JSON(http.StatusOK, resp)
}
// GetPublicHallLayout handles GET /v1/halls/:id/seats/layout for unauthenticated users.
// It returns the seating layout grouped by row along with the maximum column
// count.  Seats are retrieved from the SeatRepo.  The optional query
// parameter "active" may be supplied to filter by the seat's is_active
// flag (true or false).  If SeatRepo is nil, an internal server error
// is returned.  This endpoint does not require authentication and is
// intended for customers to view seat arrangements before selecting seats.
func (h *PublicHandler) GetPublicHallLayout(c echo.Context) error {
	if h.SeatRepo == nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "seat repository not configured"})
	}
	ctx := c.Request().Context()
	hallID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || hallID == 0 {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
	}
	// ensure hall exists
	if _, err := h.HallRepo.GetByID(ctx, hallID); err != nil {
		if err == repository.ErrHallNotFound {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "hall not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "database error"})
	}
	seats, err := h.SeatRepo.GetByHall(ctx, hallID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "database error"})
	}
	// optional active filter
	if v := strings.ToLower(strings.TrimSpace(c.QueryParam("active"))); v == "true" || v == "1" || v == "false" || v == "0" {
		want := v == "true" || v == "1"
		filtered := make([]repository.Seat, 0, len(seats))
		for _, s := range seats {
			if s.IsActive == want {
				filtered = append(filtered, s)
			}
		}
		seats = filtered
	}
	// group by row and build output similar to owner layout
	rowsMap := make(map[string][]uint32)
	maxCols := 0
	for _, s := range seats {
		lbl := strings.ToUpper(strings.TrimSpace(s.RowLabel))
		rowsMap[lbl] = append(rowsMap[lbl], s.SeatNumber)
		if int(s.SeatNumber) > maxCols {
			maxCols = int(s.SeatNumber)
		}
	}
	// order row labels using rowLabelToIndex helper if available in owner
	rowOrder := make([]string, 0, len(rowsMap))
	for lbl := range rowsMap {
		rowOrder = append(rowOrder, lbl)
	}
	sort.Slice(rowOrder, func(i, j int) bool {
		ii, okI := rowLabelToIndex(rowOrder[i])
		jj, okJ := rowLabelToIndex(rowOrder[j])
		if !okI || !okJ {
			return rowOrder[i] < rowOrder[j]
		}
		return ii < jj
	})
	type rowOut struct {
		RowLabel string   `json:"row_label"`
		Numbers  []uint32 `json:"numbers"`
	}
	rowsOut := make([]rowOut, 0, len(rowOrder))
	pretty := make([]string, 0, len(rowOrder))
	for _, lbl := range rowOrder {
		nums := rowsMap[lbl]
		sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
		rowsOut = append(rowsOut, rowOut{RowLabel: lbl, Numbers: nums})
		var b strings.Builder
		b.WriteString(lbl)
		b.WriteString(": ")
		for i, n := range nums {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.FormatUint(uint64(n), 10))
		}
		pretty = append(pretty, b.String())
	}
	return c.JSON(http.StatusOK, echo.Map{
		"hall_id":  hallID,
		"max_cols": maxCols,
		"order":    rowOrder,
		"rows":     rowsOut,
		"pretty":   pretty,
	})
}

// GetPublicShowSeats handles GET /v1/shows/:id/seats for unauthenticated users.
// It returns the status of each seat for the given show ID.  A seat is
// considered RESERVED when its show_seats.status is RESERVED.  It is
// considered HELD if there exists a non-expired seat_hold for it (held by
// any user).  Otherwise it is FREE.  The response contains an array of
// objects with seat_id, row_label, seat_number and status.
func (h *PublicHandler) GetPublicShowSeats(c echo.Context) error {
	if h.ShowSeatRepo == nil || h.SeatRepo == nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "seat repositories not configured"})
	}
	ctx := c.Request().Context()
	showID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || showID == 0 {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
	}
	// ensure show exists
	if _, err := h.ShowRepo.GetByID(ctx, showID); err != nil {
		if err == repository.ErrShowNotFound {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "show not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "database error"})
	}
	// Before fetching seat status, expire any holds that have passed
	// their expiration.  This ensures that seats with expired holds
	// become available (FREE) and are reported correctly to clients.
	if h.SeatHoldRepo != nil {
		// Run expiration cleanup in a short transaction.  Use the show seat
		// repository's DB connection to begin the transaction.
		tx, txErr := h.ShowSeatRepo.DB().BeginTx(ctx, nil)
		if txErr == nil {
			// If any holds have expired, they will be deleted and returned
			// as seat IDs.  We then update those show_seats back to FREE.
			if expired, expErr := h.SeatHoldRepo.ExpireHoldsTx(ctx, tx, showID); expErr == nil {
				if len(expired) > 0 {
					_ = h.ShowSeatRepo.BulkUpdateStatusTx(ctx, tx, showID, expired, "FREE")
				}
				// Commit regardless of whether expired were found to avoid leaving an open transaction
				_ = tx.Commit()
			} else {
				// If cleanup failed, roll back and ignore the error for seat listing
				_ = tx.Rollback()
			}
		}
	}
	seats, err := h.ShowSeatRepo.ListWithStatus(ctx, showID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "database error"})
	}
	// Build response items.  If Redis is configured, consult it for
	// active seat holds.  A Redis hold key overrides the database
	// status and sets the status to HELD.  Redis operations are
	// performed with the same request context so they can be cancelled
	// if the request times out.  If a Redis error occurs (other than
	// key not found), we ignore it and fall back to the DB status.
	type seatOut struct {
		SeatID     uint64 `json:"seat_id"`
		RowLabel   string `json:"row_label"`
		SeatNumber uint32 `json:"seat_number"`
		Status     string `json:"status"`
	}
	items := make([]seatOut, 0, len(seats))
	for _, s := range seats {
		status := s.Status
		if h.RedisClient != nil {
			key := fmt.Sprintf("hold:%d:%d", showID, s.SeatID)
			// Use Exists to check for a hold.  Returns the number of keys existing (0 or 1).
			if exists, err := h.RedisClient.Exists(ctx, key).Result(); err == nil && exists > 0 {
				status = "HELD"
			}
		}
		items = append(items, seatOut{SeatID: s.SeatID, RowLabel: s.RowLabel, SeatNumber: s.SeatNumber, Status: status})
	}
	return c.JSON(http.StatusOK, echo.Map{
		"show_id": showID,
		"count":   len(items),
		"items":   items,
	})
}

// GetPublicHallSeats handles GET /v1/halls/:id/seats for unauthenticated users.
// It returns a flat list of seats for the given hall.  Each seat entry contains
// the seat_id, row_label, seat_number, seat_type and is_active flag.  An
// optional query parameter "active" may be supplied to filter results by the
// seat's activation status (true or false).  This endpoint does not require
// authentication and allows guests to inspect the hall's seats before
// selecting a show.
func (h *PublicHandler) GetPublicHallSeats(c echo.Context) error {
	// Ensure the seat repository is configured; without it we cannot list seats.
	if h.SeatRepo == nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "seat repository not configured"})
	}
	ctx := c.Request().Context()
	hallID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || hallID == 0 {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
	}
	// Ensure the hall exists before querying its seats.  We do not expose
	// internal errors to clients but return 404 if the hall is not found.
	if _, err := h.HallRepo.GetByID(ctx, hallID); err != nil {
		if err == repository.ErrHallNotFound {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "hall not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "database error"})
	}
	// Fetch all seats for this hall ordered by row and number.
	seats, err := h.SeatRepo.GetByHall(ctx, hallID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "database error"})
	}
	// Optionally filter by the "active" query parameter.  Accepts true/false or 1/0.
	if v := strings.ToLower(strings.TrimSpace(c.QueryParam("active"))); v == "true" || v == "1" || v == "false" || v == "0" {
		want := v == "true" || v == "1"
		filtered := make([]repository.Seat, 0, len(seats))
		for _, s := range seats {
			if s.IsActive == want {
				filtered = append(filtered, s)
			}
		}
		seats = filtered
	}
	// Build the response items.  We include the seat type and active flag so
	// clients can identify special seats (e.g. VIP, ACCESSIBLE) and current
	// availability status (soft availability, not reservation status).
	type seatOut struct {
		SeatID     uint64 `json:"seat_id"`
		RowLabel   string `json:"row_label"`
		SeatNumber uint32 `json:"seat_number"`
		SeatType   string `json:"seat_type"`
		IsActive   bool   `json:"is_active"`
	}
	items := make([]seatOut, 0, len(seats))
	for _, s := range seats {
		items = append(items, seatOut{
			SeatID:     s.ID,
			RowLabel:   s.RowLabel,
			SeatNumber: s.SeatNumber,
			SeatType:   s.SeatType,
			IsActive:   s.IsActive,
		})
	}
	return c.JSON(http.StatusOK, echo.Map{
		"hall_id": hallID,
		"count":   len(items),
		"items":   items,
	})
}