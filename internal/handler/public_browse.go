// Package handler exposes HTTP handlers for both authenticated and public endpoints.
// This file defines handlers for the public browsing API. These routes allow
// unauthenticated users to browse cinemas, halls and shows without requiring
// authentication. Sensitive fields (owner IDs, timestamps, etc.) are filtered
// from responses.

package handler

import (
    "net/http"  // HTTP status codes and request context
    "strconv"   // string to integer conversion utilities
    "strings"   // trimming and other string helpers
    "time"      // parsing and formatting timestamps

    "github.com/labstack/echo/v4"                         // Echo web framework
    "github.com/iliyamo/cinema-seat-reservation/internal/repository" // repository interfaces
)

// PublicHandler aggregates repositories needed for unauthenticated browsing.
// It produces sanitized responses suitable for public consumption.
type PublicHandler struct {
    CinemaRepo *repository.CinemaRepo // provides access to cinema data
    HallRepo   *repository.HallRepo   // provides access to hall data
    ShowRepo   *repository.ShowRepo   // provides access to show data
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
    ID        uint64  `json:"id"`        // ID uniquely identifies the show
    Title     string  `json:"title"`     // Title is the movie or event name
    // StartTime is the ISO 8601 formatted start time of the show. It is a pointer
    // so that null values can be encoded as JSON null. The omitempty directive is
    // omitted to ensure the field always appears in the response, even when nil.
    StartTime *string `json:"start_time"`
    // EndTime is the ISO 8601 formatted end time of the show. Like StartTime,
    // it is a pointer to allow null values when no end time is provided. The
    // absence of omitempty causes the field to appear with a null value when nil.
    EndTime   *string `json:"end_time"`
}

// PublicShowDetail represents a single show with related cinema and hall names.
// StartTime and EndTime are ISO 8601 strings or null pointers. Only
// non-sensitive fields are returned.
type PublicShowDetail struct {
    ID        uint64        `json:"id"`         // show identifier
    Title     string        `json:"title"`      // movie or event title
    // StartTime is the ISO 8601 formatted start time or null. It is not
    // tagged with omitempty so it will always appear in the JSON output.
    StartTime *string       `json:"start_time"`
    // EndTime is the ISO 8601 formatted end time or null.
    EndTime   *string       `json:"end_time"`
    // Cinema contains the minimal cinema info (id, name) if available.
    Cinema    *PublicCinema `json:"cinema,omitempty"`
    // Hall contains the minimal hall info (id, name) if available.
    Hall      *struct {
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
        var startPtr, endPtr *string
        // parse and format start time if present
        if ts := strings.TrimSpace(s.StartsAt); ts != "" && ts != "0001-01-01 00:00:00" {
            if t, parseErr := time.Parse("2006-01-02 15:04:05", ts); parseErr == nil {
                iso := t.UTC().Format(time.RFC3339)
                startPtr = &iso
            }
        }
        // parse and format end time if present
        if te := strings.TrimSpace(s.EndsAt); te != "" && te != "0001-01-01 00:00:00" {
            if et, parseErr := time.Parse("2006-01-02 15:04:05", te); parseErr == nil {
                iso := et.UTC().Format(time.RFC3339)
                endPtr = &iso
            }
        }
        out = append(out, PublicShow{ID: s.ID, Title: s.Title, StartTime: startPtr, EndTime: endPtr})
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
    // parse start and end times; assign nil pointers if invalid or zero
    var startPtr, endPtr *string
    if ts := strings.TrimSpace(s.StartsAt); ts != "" && ts != "0001-01-01 00:00:00" {
        if t, parseErr := time.Parse("2006-01-02 15:04:05", ts); parseErr == nil {
            iso := t.UTC().Format(time.RFC3339)
            startPtr = &iso
        }
    }
    if te := strings.TrimSpace(s.EndsAt); te != "" && te != "0001-01-01 00:00:00" {
        if et, parseErr := time.Parse("2006-01-02 15:04:05", te); parseErr == nil {
            iso := et.UTC().Format(time.RFC3339)
            endPtr = &iso
        }
    }
    resp := PublicShowDetail{ID: s.ID, Title: s.Title, StartTime: startPtr, EndTime: endPtr}
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