package handler

import (
    "net/http"
    "strconv"
    "strings"

    "github.com/labstack/echo/v4"

    "github.com/iliyamo/cinema-seat-reservation/internal/repository"
)

// time: "upcoming" (default), "active" (ends_at >= NOW()), "any" (no time filter)
func (h *PublicHandler) SearchShows(c echo.Context) error {
    title := strings.TrimSpace(c.QueryParam("title"))
    cinema := strings.TrimSpace(c.QueryParam("cinema"))
    hall := strings.TrimSpace(c.QueryParam("hall"))
    timeFilter := strings.ToLower(strings.TrimSpace(c.QueryParam("time")))
    if timeFilter == "" {
        timeFilter = "upcoming"
    }

    page, _ := strconv.Atoi(c.QueryParam("page"))
    if page < 1 { page = 1 }
    ps, _ := strconv.Atoi(c.QueryParam("page_size"))
    if ps < 1 { ps = 20 }
    if ps > 100 { ps = 100 }

    q := repository.ShowSearchQuery{
        Title:      title,
        Cinema:     cinema,
        Hall:       hall,
        TimeFilter: timeFilter,
        Page:       page,
        PageSize:   ps,
    }

    items, total, err := h.ShowRepo.SearchUpcoming(c.Request().Context(), q)
    if err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{
            "error":   "database_error",
            "message": err.Error(),
        })
    }

    return c.JSON(http.StatusOK, echo.Map{
        "data":      items,
        "total":     total,
        "page":      page,
        "page_size": ps,
    })
}
