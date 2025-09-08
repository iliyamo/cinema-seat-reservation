// Package handler defines HTTP handlers for authenticated OWNER operations.
// This file implements DELETE endpoints allowing an owner to remove
// cinemas, halls and shows that they own. Cascading deletions are
// performed in the repository layer to ensure dependent records are
// cleaned up. Appropriate HTTP status codes are returned when
// resources are missing, not owned by the current user or cannot
// be deleted due to conflicts (e.g. existing reservations).
package handler

import (
    "database/sql"                                      // sentinel errors such as sql.ErrNoRows
    "net/http"                                         // status code constants
    "strconv"                                          // string-to-integer conversion

    "github.com/iliyamo/cinema-seat-reservation/internal/repository" // repository defines error types
    "github.com/labstack/echo/v4"                                   // echo provides request/response handling
)

// DeleteCinema handles DELETE /v1/cinemas/:id. It removes the specified cinema
// and all dependent records if it belongs to the authenticated owner. A
// successful deletion returns 204 No Content. If the cinema does not
// exist, a 404 Not Found is returned. If it exists but belongs to
// another owner, 403 Forbidden is returned.
func (h *OwnerHandler) DeleteCinema(c echo.Context) error {
    ownerID, err := getUserID(c)
    if err != nil {
        return c.JSON(http.StatusUnauthorized, echo.Map{"error": "unauthorized"})
    }
    id, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil {
        return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
    }
    err = h.CinemaRepo.DeleteByIDAndOwner(c.Request().Context(), id, ownerID)
    if err != nil {
        switch err {
        case sql.ErrNoRows:
            return c.JSON(http.StatusNotFound, echo.Map{"error": "cinema not found"})
        case repository.ErrForbidden:
            return c.JSON(http.StatusForbidden, echo.Map{"error": "forbidden"})
        default:
            return c.JSON(http.StatusInternalServerError, echo.Map{"error": "delete failed"})
        }
    }
    return c.NoContent(http.StatusNoContent)
}

// DeleteHall handles DELETE /v1/halls/:id. It removes a hall and its dependent
// seats, shows, show seats, reservations and reservation seats for the
// authenticated owner. Returns 204 on success, 404 if not found and
// 403 if the hall belongs to another owner.
func (h *OwnerHandler) DeleteHall(c echo.Context) error {
    ownerID, err := getUserID(c)
    if err != nil {
        return c.JSON(http.StatusUnauthorized, echo.Map{"error": "unauthorized"})
    }
    id, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil {
        return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
    }
    err = h.HallRepo.DeleteByIDAndOwner(c.Request().Context(), id, ownerID)
    if err != nil {
        switch err {
        case sql.ErrNoRows:
            return c.JSON(http.StatusNotFound, echo.Map{"error": "hall not found"})
        case repository.ErrForbidden:
            return c.JSON(http.StatusForbidden, echo.Map{"error": "forbidden"})
        default:
            return c.JSON(http.StatusInternalServerError, echo.Map{"error": "delete failed"})
        }
    }
    return c.NoContent(http.StatusNoContent)
}

// DeleteShow handles DELETE /v1/shows/:id. It removes a show and all show seats
// associated with it if the show belongs to a hall owned by the
// authenticated user. If there are reservations for the show, the
// deletion is aborted and a 409 Conflict response is returned. A 404
// Not Found is returned when the show does not exist. A 403 Forbidden
// is returned when the show belongs to another owner.
func (h *OwnerHandler) DeleteShow(c echo.Context) error {
    ownerID, err := getUserID(c)
    if err != nil {
        return c.JSON(http.StatusUnauthorized, echo.Map{"error": "unauthorized"})
    }
    id, err := strconv.ParseUint(c.Param("id"), 10, 64)
    if err != nil {
        return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid id"})
    }
    err = h.ShowRepo.DeleteByIDAndOwner(c.Request().Context(), id, ownerID)
    if err != nil {
        switch err {
        case repository.ErrShowNotFound:
            // Not found sentinel is defined in show repository
            return c.JSON(http.StatusNotFound, echo.Map{"error": "show not found"})
        case sql.ErrNoRows:
            // In case DeleteByIDAndOwner uses sql.ErrNoRows for not found
            return c.JSON(http.StatusNotFound, echo.Map{"error": "show not found"})
        case repository.ErrForbidden:
            return c.JSON(http.StatusForbidden, echo.Map{"error": "forbidden"})
        case repository.ErrConflict:
            return c.JSON(http.StatusConflict, echo.Map{"error": "cannot delete show with reservations"})
        default:
            return c.JSON(http.StatusInternalServerError, echo.Map{"error": "delete failed"})
        }
    }
    return c.NoContent(http.StatusNoContent)
}