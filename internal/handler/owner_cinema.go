package handler // handler package contains owner-specific cinema handlers

import (
    "database/sql"                                              // sql is imported for sentinel errors like sql.ErrNoRows
    "net/http"                                                // http provides status code constants
    "strconv"                                                // strconv parses string identifiers to numeric types
    "strings"                                                // strings offers trimming utilities

    "github.com/iliyamo/cinema-seat-reservation/internal/repository" // repository holds database models
    "github.com/labstack/echo/v4"                                   // echo is the web framework used for handlers
)

// CreateCinema handles POST /v1/cinemas and creates a new cinema for the authenticated owner
func (h *OwnerHandler) CreateCinema(c echo.Context) error { // begin CreateCinema handler
    ownerID, err := getUserID(c) // extract the owner ID from context
    if err != nil { // check if the user ID was not available or invalid
        return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // respond with unauthorized when user ID cannot be obtained
    }
    var body struct { // anonymous struct to bind incoming JSON
        Name string `json:"name"` // Name is the only required field for a cinema
    }
    if err := c.Bind(&body); err != nil { // attempt to bind the request body into the struct
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"}) // return bad request when binding fails
    }
    name := strings.TrimSpace(body.Name) // trim spaces around the cinema name
    if name == "" { // ensure the name is not empty after trimming
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"}) // respond with error when name is empty
    }
    cinema := &repository.Cinema{ // instantiate a new cinema model
        OwnerID: ownerID, // assign the owner ID to the cinema
        Name:    name,    // assign the trimmed name
    }
    if err := h.CinemaRepo.Create(c.Request().Context(), cinema); err != nil { // delegate creation to the repository
        if strings.Contains(err.Error(), "1062") { // check for duplicate key error
            return c.JSON(http.StatusConflict, map[string]string{"error": "cinema name already exists"}) // respond with conflict when the name is not unique
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not create cinema"}) // respond with internal error for other failures
    }
    return c.JSON(http.StatusCreated, cinema) // return 201 and the created cinema on success
}

// UpdateCinema handles PUT/PATCH /v1/cinemas/:id and updates the cinema name
func (h *OwnerHandler) UpdateCinema(c echo.Context) error { // begin UpdateCinema handler
    ownerID, err := getUserID(c) // extract the owner ID from context
    if err != nil { // if user ID is invalid
        return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // unauthorized error
    }
    id, err := strconv.ParseUint(c.Param("id"), 10, 64) // parse the cinema ID from the URL
    if err != nil { // validate that the ID is numeric
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"}) // invalid ID error response
    }
    var body struct { // struct for binding the JSON payload
        Name string `json:"name"` // Name is the only updatable field
    }
    if err := c.Bind(&body); err != nil { // attempt to bind the request body
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"}) // return bad request when binding fails
    }
    name := strings.TrimSpace(body.Name) // trim spaces from the provided name
    if name == "" { // name cannot be empty after trimming
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "name is required"}) // respond with bad request if name is empty
    }
    if _, err := h.CinemaRepo.GetByIDAndOwner(c.Request().Context(), id, ownerID); err != nil { // verify the cinema exists and belongs to the owner
        if err == repository.ErrCinemaNotFound { // when the cinema is not found
            return c.JSON(http.StatusNotFound, map[string]string{"error": "cinema not found"}) // respond with not found
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"}) // respond with database error
    }
    if err := h.CinemaRepo.UpdateName(c.Request().Context(), id, ownerID, name); err != nil { // update the cinema name in the repository
        if err == sql.ErrNoRows { // no rows affected means not found
            return c.JSON(http.StatusNotFound, map[string]string{"error": "cinema not found"}) // respond with not found
        }
        if strings.Contains(err.Error(), "1062") { // duplicate name violation
            return c.JSON(http.StatusConflict, map[string]string{"error": "cinema name already exists"}) // respond with conflict
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "update failed"}) // respond with generic update failure
    }
    updated, _ := h.CinemaRepo.GetByID(c.Request().Context(), id) // fetch the updated record without ownership filter
    return c.JSON(http.StatusOK, updated) // return the updated cinema with OK status
}

// ListCinemas handles GET /v1/cinemas and returns all cinemas owned by the authenticated user
func (h *OwnerHandler) ListCinemas(c echo.Context) error { // begin ListCinemas handler
    ownerID, err := getUserID(c) // extract the user ID from context
    if err != nil { // invalid user means unauthorized
        return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // respond unauthorized
    }
    items, err := h.CinemaRepo.ListByOwner(c.Request().Context(), ownerID) // fetch cinemas for this owner
    if err != nil { // handle repository errors
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"}) // respond with internal server error
    }
    return c.JSON(http.StatusOK, map[string]any{"items": items}) // return the list wrapped in a JSON object
}