package handler // handler package contains owner-specific seat handlers

import (
    "database/sql"                                              // sql provides sentinel errors for comparison
    "net/http"                                                // http defines status code constants
    "strconv"                                                // strconv parses identifiers from path params
    "strings"                                                // strings manipulates text and case

    "github.com/iliyamo/cinema-seat-reservation/internal/repository" // repository defines data models
    "github.com/labstack/echo/v4"                                   // echo framework provides context and JSON helpers
)

// CreateSeat handles POST /v1/seats and adds a single seat to an existing hall.  It auto-expands the hall when necessary.
func (h *OwnerHandler) CreateSeat(c echo.Context) error { // begin CreateSeat handler
    ownerID, err := getUserID(c) // extract user ID from context
    if err != nil { // user ID missing or invalid
        return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // respond unauthorized
    }
    var body struct { // structure to bind JSON body
        HallID     uint64  `json:"hall_id"`     // required hall identifier
        Row        string  `json:"row"`         // legacy row field
        RowLabel   string  `json:"row_label"`   // preferred row label field
        Number     *uint32 `json:"number"`      // legacy seat number field
        SeatNumber *uint32 `json:"seat_number"` // preferred seat number field
        Type       string  `json:"type"`        // legacy seat type field
        SeatType   string  `json:"seat_type"`   // preferred seat type field
    }
    if err := c.Bind(&body); err != nil { // bind incoming JSON
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"}) // respond bad request when binding fails
    }
    if body.HallID == 0 { // hall ID must be specified
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "hall_id is required"}) // respond when hall ID is zero
    }
    // determine row label: prefer RowLabel but fall back to Row
    rawLabel := strings.TrimSpace(body.RowLabel) // trim whitespace from RowLabel
    if rawLabel == "" { // when RowLabel is empty
        rawLabel = strings.TrimSpace(body.Row) // use legacy Row field
    }
    rowLabel := normalizeRowLabel(rawLabel) // sanitize row label to uppercase ASCII letters only
    if rowLabel == "" { // row label is still empty after normalization
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "row_label is required"}) // respond with validation error
    }
    // determine seat number from either SeatNumber or Number
    var seatNum uint32 // hold the resolved seat number
    if body.SeatNumber != nil { // preferred field present
        seatNum = *body.SeatNumber // use provided value
    } else if body.Number != nil { // legacy field present
        seatNum = *body.Number // use legacy value
    }
    if seatNum == 0 { // seat number must be positive
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "seat_number is required and must be greater than zero"}) // respond invalid number
    }
    // normalize seat type; allow empty, STANDARD, VIP, ACCESSIBLE, DISABLED
    seatType := strings.ToUpper(strings.TrimSpace(body.SeatType)) // normalize preferred field
    if seatType == "" { // if preferred field missing
        seatType = strings.ToUpper(strings.TrimSpace(body.Type)) // use legacy field
    }
    switch seatType { // validate seat type values
    case "", "STANDARD", "VIP", "ACCESSIBLE": // allowed values
        if seatType == "" { // default to STANDARD when empty
            seatType = "STANDARD" // assign default seat type
        }
    case "DISABLED": // map DISABLED to ACCESSIBLE
        seatType = "ACCESSIBLE" // assign accessible seat type
    default: // any other string is invalid
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid seat type"}) // respond invalid type
    }
    hall, err := h.HallRepo.GetByIDAndOwner(c.Request().Context(), body.HallID, ownerID) // load the hall to verify ownership
    if err != nil { // handle hall retrieval error
        if err == repository.ErrHallNotFound { // hall not found for owner
            return c.JSON(http.StatusNotFound, map[string]string{"error": "hall not found"}) // respond not found
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not verify hall"}) // respond generic error
    }
    // convert row label to index for expansion calculation
    reqRowIdx, ok := rowLabelToIndex(rowLabel) // convert row label to zero-based index
    if !ok { // invalid row label when conversion fails
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid row_label"}) // respond invalid row label
    }
    // determine current seat capacity of the hall
    curRows := uint32(0) // default when seat rows are nil
    curCols := uint32(0) // default when seat cols are nil
    if hall.SeatRows.Valid { // check if rows field has a value
        curRows = uint32(hall.SeatRows.Int32) // convert current rows to uint32
    }
    if hall.SeatCols.Valid { // check if cols field has a value
        curCols = uint32(hall.SeatCols.Int32) // convert current columns to uint32
    }
    needsExpand := uint32(reqRowIdx+1) > curRows || seatNum > curCols // determine if hall layout must be expanded
    if needsExpand { // perform hall expansion when needed
        newRows := curRows // start with current row count
        newCols := curCols // start with current column count
        if uint32(reqRowIdx+1) > newRows { // if requested row index exceeds current rows
            newRows = uint32(reqRowIdx + 1) // update rows to accommodate the new row
        }
        if seatNum > newCols { // if requested seat number exceeds current columns
            newCols = seatNum // update columns to accommodate the new seat number
        }
        upd := &repository.Hall{ // build hall update model
            ID:          hall.ID,                                                             // hall identifier
            OwnerID:     ownerID,                                                             // owner ID for verification
            CinemaID:    hall.CinemaID,                                                       // preserve cinema linkage
            Name:        hall.Name,                                                           // preserve name
            Description: hall.Description,                                                    // preserve description
            SeatRows:    sql.NullInt32{Int32: int32(newRows), Valid: true},                   // set updated row count
            SeatCols:    sql.NullInt32{Int32: int32(newCols), Valid: true},                   // set updated column count
            IsActive:    hall.IsActive,                                                       // preserve active status
            CreatedAt:   hall.CreatedAt,                                                      // preserve creation time
            UpdatedAt:   hall.UpdatedAt,                                                      // preserve update time
        }
        if err := h.HallRepo.UpdateByIDAndOwner(c.Request().Context(), upd); err != nil { // update hall to reflect new capacity
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to expand hall capacity"}) // respond failure expanding hall
        }
        existing, err := h.SeatRepo.GetByHall(c.Request().Context(), hall.ID) // load existing seats to identify which exist
        if err != nil { // handle error loading seats
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load seats"}) // respond error
        }
        type key struct { // composite key used to check existing seats
            r string // row label in uppercase
            n uint32 // seat number
        }
        exists := make(map[key]struct{}, len(existing)) // map to record seats that already exist
        for _, s := range existing { // populate the map with current seats
            exists[key{r: strings.ToUpper(s.RowLabel), n: s.SeatNumber}] = struct{}{} // mark seat as existing
        }
        toCreate := make([]repository.Seat, 0, int(newRows*newCols)) // slice to collect new seats
        for r := uint32(0); r < newRows; r++ { // iterate through row indices
            lbl := indexToRowLabel(int(r)) // compute the row label
            for n := uint32(1); n <= newCols; n++ { // iterate seat numbers starting at 1
                if strings.EqualFold(lbl, rowLabel) && n == seatNum { // skip the requested seat to be created separately
                    continue // do not backfill the requested seat
                }
                if _, ok := exists[key{r: lbl, n: n}]; ok { // if seat already exists
                    continue // skip existing seats
                }
                toCreate = append(toCreate, repository.Seat{ // append a default seat definition
                    HallID:     hall.ID,           // assign hall ID
                    RowLabel:   lbl,               // assign computed row label
                    SeatNumber: n,                 // assign seat number
                    SeatType:   "STANDARD",       // default seat type
                })
            }
        }
        if err := h.SeatRepo.CreateBulk(c.Request().Context(), toCreate); err != nil { // insert backfill seats
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to backfill seats after expanding hall"}) // respond error when insertion fails
        }
    }
    seat := &repository.Seat{ // build the seat model to insert
        HallID:     body.HallID, // assign hall ID
        RowLabel:   rowLabel,    // assign normalized row label
        SeatNumber: seatNum,     // assign seat number
        SeatType:   seatType,    // assign seat type
    }
    if err := h.SeatRepo.Create(c.Request().Context(), seat); err != nil { // attempt to create the requested seat
        if strings.Contains(err.Error(), "1062") { // duplicate entry error indicates seat exists
            return c.JSON(http.StatusConflict, map[string]string{"error": "seat already exists"}) // respond conflict when seat duplicates existing
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not create seat"}) // respond generic error when creation fails
    }
    // fetch the full seat including timestamps after creation
    full, err := h.SeatRepo.GetByID(c.Request().Context(), seat.ID) // load the inserted seat
    if err != nil { // handle error fetching seat
        // if retrieval fails, still return the partially populated seat
        return c.JSON(http.StatusCreated, seat) // respond with created seat without timestamps
    }
    return c.JSON(http.StatusCreated, full) // return the fully populated seat with timestamps
}

// UpdateSeat handles PUT/PATCH /v1/seats/:id and modifies seat attributes.  It can relocate a seat and expand the hall if necessary.
func (h *OwnerHandler) UpdateSeat(c echo.Context) error { // begin UpdateSeat handler
    ownerID, err := getUserID(c) // retrieve user ID
    if err != nil { // unauthorized when user ID is invalid
        return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // respond unauthorized
    }
    id, err := strconv.ParseUint(c.Param("id"), 10, 64) // parse seat ID from path
    if err != nil { // invalid seat ID
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"}) // respond invalid id
    }
    var body struct { // structure to bind JSON body
        RowLabel   string  `json:"row_label"` // new row label
        SeatNumber uint32  `json:"seat_number"` // new seat number
        SeatType   *string `json:"seat_type"` // optional new seat type
        IsActive   *bool   `json:"is_active"` // optional active flag
    }
    if err := c.Bind(&body); err != nil { // bind incoming JSON
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"}) // respond bad request when binding fails
    }
    rowLabel := normalizeRowLabel(body.RowLabel) // sanitize the row label
    if rowLabel == "" || body.SeatNumber == 0 { // row label and seat number are mandatory
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "row_label and seat_number are required"}) // respond validation error
    }
    var normalizedType string // local variable for the normalized seat type
    if body.SeatType != nil { // seat type provided
        normalizedType = strings.ToUpper(strings.TrimSpace(*body.SeatType)) // normalize provided seat type
        switch normalizedType { // validate allowed values
        case "", "STANDARD", "VIP", "ACCESSIBLE": // allowed values including empty which defaults to STANDARD
            if normalizedType == "" { // empty string defaults
                normalizedType = "STANDARD" // assign default seat type
            }
        case "DISABLED": // map DISABLED to ACCESSIBLE
            normalizedType = "ACCESSIBLE" // assign accessible seat type
        default: // invalid seat type detected
            return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid seat type"}) // respond invalid seat type
        }
    }
    curSeat, err := h.SeatRepo.GetByIDAndOwner(c.Request().Context(), id, ownerID) // load the current seat to verify ownership
    if err != nil { // handle retrieval errors
        if err == repository.ErrSeatNotFound { // seat not found for owner
            return c.JSON(http.StatusNotFound, map[string]string{"error": "seat not found"}) // respond not found
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"}) // generic database error
    }
    isActive := curSeat.IsActive // start with current active flag
    if body.IsActive != nil { // update when provided
        isActive = *body.IsActive // assign new value
    }
    hall, err := h.HallRepo.GetByIDAndOwner(c.Request().Context(), curSeat.HallID, ownerID) // fetch the hall owning the seat
    if err != nil { // handle hall retrieval errors
        if err == repository.ErrHallNotFound { // hall not found for owner
            return c.JSON(http.StatusNotFound, map[string]string{"error": "hall not found"}) // respond not found
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"}) // generic database error
    }
    reqRowIdx, ok := rowLabelToIndex(rowLabel) // convert row label to index for expansion logic
    if !ok { // invalid row label conversion
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid row_label"}) // respond invalid row label
    }
    curRows := uint32(0) // current row count in hall
    curCols := uint32(0) // current column count in hall
    if hall.SeatRows.Valid { // hall has a row count specified
        curRows = uint32(hall.SeatRows.Int32) // convert to uint32
    }
    if hall.SeatCols.Valid { // hall has a column count specified
        curCols = uint32(hall.SeatCols.Int32) // convert to uint32
    }
    needsExpand := uint32(reqRowIdx+1) > curRows || body.SeatNumber > curCols // determine if hall expansion is necessary
    if needsExpand { // perform hall expansion when seat moves beyond current layout
        newRows := curRows // start with current rows
        newCols := curCols // start with current cols
        if uint32(reqRowIdx+1) > newRows { // if requested row index exceeds current rows
            newRows = uint32(reqRowIdx + 1) // set new rows to accommodate
        }
        if body.SeatNumber > newCols { // if requested seat number exceeds current cols
            newCols = body.SeatNumber // set new cols accordingly
        }
        updHall := &repository.Hall{ // build hall update model
            ID:          hall.ID,                                                                  // hall ID
            OwnerID:     ownerID,                                                                 // owner ID
            CinemaID:    hall.CinemaID,                                                           // preserve cinema ID
            Name:        hall.Name,                                                               // preserve hall name
            Description: hall.Description,                                                        // preserve description
            SeatRows:    sql.NullInt32{Int32: int32(newRows), Valid: true},                       // update row count
            SeatCols:    sql.NullInt32{Int32: int32(newCols), Valid: true},                       // update column count
            IsActive:    hall.IsActive,                                                           // preserve active flag
            CreatedAt:   hall.CreatedAt,                                                          // preserve creation time
            UpdatedAt:   hall.UpdatedAt,                                                          // preserve update time
        }
        if err := h.HallRepo.UpdateByIDAndOwner(c.Request().Context(), updHall); err != nil { // persist hall expansion
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to expand hall capacity"}) // respond error on failure
        }
        existing, err := h.SeatRepo.GetByHall(c.Request().Context(), hall.ID) // load existing seats to know which seats exist
        if err != nil { // handle error loading seats
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load seats"}) // respond error
        }
        type key struct { // key to index existing seats
            r string // row label in uppercase
            n uint32 // seat number
        }
        exists := make(map[key]struct{}, len(existing)) // map to record existing seats
        for _, s := range existing { // populate map with current seats
            exists[key{r: strings.ToUpper(s.RowLabel), n: s.SeatNumber}] = struct{}{} // mark seat as existing
        }
        toCreate := make([]repository.Seat, 0, int(newRows*newCols)) // slice to collect seats to create
        for r := uint32(0); r < newRows; r++ { // iterate rows
            lbl := indexToRowLabel(int(r)) // compute row label
            for n := uint32(1); n <= newCols; n++ { // iterate seat numbers
                if strings.EqualFold(lbl, rowLabel) && n == body.SeatNumber { // skip the seat being updated
                    continue // do not backfill the new seat position
                }
                if _, ok := exists[key{r: lbl, n: n}]; ok { // skip seats that already exist
                    continue // seat already present
                }
                toCreate = append(toCreate, repository.Seat{ // append a default seat record
                    HallID:     hall.ID,           // hall ID
                    RowLabel:   lbl,               // row label
                    SeatNumber: n,                 // seat number
                    SeatType:   "STANDARD",       // default seat type
                })
            }
        }
        if len(toCreate) > 0 { // if there are seats to create
            if err := h.SeatRepo.CreateBulk(c.Request().Context(), toCreate); err != nil { // insert missing seats
                return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to backfill seats after expanding hall"}) // respond error on failure
            }
        }
    }
    if body.SeatType != nil { // seat type is being updated
        if err := h.SeatRepo.UpdateWithTypeByIDAndOwner(c.Request().Context(), id, ownerID, rowLabel, body.SeatNumber, normalizedType, isActive); err != nil { // update seat with type
            if err == sql.ErrNoRows { // seat not found during update
                return c.JSON(http.StatusNotFound, map[string]string{"error": "seat not found"}) // respond not found
            }
            if strings.Contains(err.Error(), "1062") { // duplicate seat error
                return c.JSON(http.StatusConflict, map[string]string{"error": "seat already exists"}) // respond conflict
            }
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "update failed"}) // generic update error
        }
    } else { // seat type is not being updated
        if err := h.SeatRepo.UpdateByIDAndOwner(c.Request().Context(), id, ownerID, rowLabel, body.SeatNumber, isActive); err != nil { // update seat without type change
            if err == sql.ErrNoRows { // seat not found
                return c.JSON(http.StatusNotFound, map[string]string{"error": "seat not found"}) // respond not found
            }
            if strings.Contains(err.Error(), "1062") { // duplicate seat placement
                return c.JSON(http.StatusConflict, map[string]string{"error": "seat already exists"}) // respond conflict
            }
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "update failed"}) // generic update error
        }
    }
    updated, err := h.SeatRepo.GetByIDAndOwner(c.Request().Context(), id, ownerID) // retrieve the updated seat
    if err != nil { // handle fetch error after update
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load updated seat"}) // respond error when unable to load seat
    }
    return c.JSON(http.StatusOK, updated) // return the updated seat with OK status
}

// DeleteSeat handles DELETE /v1/seats/:id and removes a seat belonging to the owner.
func (h *OwnerHandler) DeleteSeat(c echo.Context) error { // begin DeleteSeat handler
    ownerID, err := getUserID(c) // extract user ID
    if err != nil { // user not authenticated
        return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // respond unauthorized
    }
    id, err := strconv.ParseUint(c.Param("id"), 10, 64) // parse seat ID from path
    if err != nil { // invalid seat ID provided
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"}) // respond invalid id
    }
    if err := h.SeatRepo.DeleteByIDAndOwner(c.Request().Context(), id, ownerID); err != nil { // attempt to delete seat ensuring ownership
        if err == sql.ErrNoRows { // seat not found or not owned
            return c.JSON(http.StatusNotFound, map[string]string{"error": "seat not found"}) // respond not found
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "delete failed"}) // generic delete failure
    }
    return c.NoContent(http.StatusNoContent) // respond with 204 No Content on success
}