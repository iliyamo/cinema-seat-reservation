// Package handler exposes HTTP handlers for both authenticated and public endpoints.
// This file defines handlers for the public browsing API. These routes allow
// unauthenticated users to browse cinemas, halls and shows without requiring
// authentication. Sensitive fields (owner IDs, timestamps, etc.) are filtered
// from responses.

package handler

import (
    "net/http"
    "strconv"
    "time"

    "github.com/labstack/echo/v4"
    "github.com/iliyamo/cinema-seat-reservation/internal/repository"
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

// PublicShow represents a show in list responses. StartTime is parsed into
// a time.Time for better client handling. Zero values indicate an invalid parse.
type PublicShow struct {
    ID        uint64    `json:"id"`
    Title     string    `json:"title"`
    StartTime time.Time `json:"start_time"`
}

// PublicShowDetail represents a detailed show response with cinema and hall names.
type PublicShowDetail struct {
    ID        uint64        `json:"id"`
    Title     string        `json:"title"`
    StartTime time.Time     `json:"start_time"`
    Cinema    *PublicCinema `json:"cinema,omitempty"`
    Hall      *struct {
        ID   uint64 `json:"id"`
        Name string `json:"name"`
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
        // parse start time string into time.Time; if parse fails, zero time is used
        t, parseErr := time.Parse("2006-01-02 15:04:05", s.StartsAt)
        if parseErr != nil {
            t = time.Time{}
        }
        out = append(out, PublicShow{ID: s.ID, Title: s.Title, StartTime: t})
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
    // parse start time
    startTime, _ := time.Parse("2006-01-02 15:04:05", s.StartsAt)
    resp := PublicShowDetail{ID: s.ID, Title: s.Title, StartTime: startTime}
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