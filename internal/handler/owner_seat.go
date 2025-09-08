package handler // handler package contains owner-specific seat handlers

import (
	"database/sql" // sql provides sentinel errors for comparison
	"net/http"     // http defines status code constants
	"strconv"      // strconv parses identifiers from path params
	"strings"      // strings manipulates text and case

	"github.com/iliyamo/cinema-seat-reservation/internal/repository" // repository defines data models
	"github.com/labstack/echo/v4"                                    // echo framework provides context and JSON helpers
)

// CreateSeat handles POST /v1/seats and adds a single seat to an existing hall.  It auto-expands the hall when necessary.
func (h *OwnerHandler) CreateSeat(c echo.Context) error { // begin CreateSeat handler
	ownerID, err := getUserID(c) // extract user ID from context
	if err != nil {              // user ID missing or invalid
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
	if body.HallID == 0 { // hall id is required
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "hall_id is required"}) // respond invalid hall id
	}
	// normalize row label; prefer RowLabel then fallback to Row
	rowLabel := strings.ToUpper(strings.TrimSpace(body.RowLabel)) // normalize preferred row label
	if rowLabel == "" {                                           // when preferred is empty
		rowLabel = strings.ToUpper(strings.TrimSpace(body.Row)) // fallback to legacy
	}
	if rowLabel == "" { // both row fields empty
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "row_label is required"}) // respond missing row
	}
	// determine seat number; prefer SeatNumber then fallback to Number
	var seatNum uint32          // local variable for seat number
	if body.SeatNumber != nil { // preferred field present
		seatNum = *body.SeatNumber // use preferred value
	} else if body.Number != nil { // legacy field present
		seatNum = *body.Number // use legacy value
	}
	if seatNum == 0 { // seat number must be positive
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "seat_number is required and must be greater than zero"}) // respond invalid number
	}
	// normalize seat type; allow empty, STANDARD, VIP, ACCESSIBLE, DISABLED
	seatType := strings.ToUpper(strings.TrimSpace(body.SeatType)) // normalize preferred field
	if seatType == "" {                                           // if preferred field missing
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
	if err != nil {                                                                      // handle hall retrieval error
		if err == repository.ErrHallNotFound { // hall not found for owner
			return c.JSON(http.StatusNotFound, map[string]string{"error": "hall not found"}) // respond not found
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not verify hall"}) // respond generic error
	}
	// convert row label to index for expansion calculation
	reqRowIdx, ok := rowLabelToIndex(rowLabel) // convert row label to zero-based index
	if !ok {                                   // invalid row label when conversion fails
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid row_label"}) // respond invalid label
	}
	var curRows, curCols uint32 // current hall grid
	if hall.SeatRows.Valid {    // if hall stores row count
		curRows = uint32(hall.SeatRows.Int32) // convert to unsigned
	}
	if hall.SeatCols.Valid { // if hall stores column count
		curCols = uint32(hall.SeatCols.Int32) // convert to unsigned
	}
	// determine expansion need
	needExpand := uint32(reqRowIdx+1) > curRows || seatNum > curCols // expansion required if either dimension exceeded
	if needExpand {                                                  // perform expansion
		newRows := curRows                 // start with current
		newCols := curCols                 // start with current
		if uint32(reqRowIdx+1) > newRows { // adjust rows
			newRows = uint32(reqRowIdx + 1) // extend rows
		}
		if seatNum > newCols { // adjust columns
			newCols = seatNum // extend columns
		}
		upd := &repository.Hall{ // hall update payload
			ID:          hall.ID,                                           // hall ID
			OwnerID:     hall.OwnerID,                                      // owner ID
			CinemaID:    hall.CinemaID,                                     // cinema ID
			Name:        hall.Name,                                         // keep name
			Description: hall.Description,                                  // keep desc
			SeatRows:    sql.NullInt32{Int32: int32(newRows), Valid: true}, // set new rows
			SeatCols:    sql.NullInt32{Int32: int32(newCols), Valid: true}, // set new cols
			IsActive:    hall.IsActive,                                     // keep flag
			CreatedAt:   hall.CreatedAt,                                    // keep ts
			UpdatedAt:   hall.UpdatedAt,                                    // keep ts
		}
		if err := h.HallRepo.UpdateByIDAndOwner(c.Request().Context(), upd); err != nil { // persist expansion
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to expand hall capacity"}) // respond failure
		}
		// backfill missing seats in grid (excluding the one being created)
		existing, err := h.SeatRepo.GetByHall(c.Request().Context(), hall.ID) // load existing seats for the hall
		if err != nil {                                                       // error when reading seats
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load seats"}) // respond failure
		}
		type key struct {
			r string
			n uint32
		} // composite key
		exists := make(map[key]struct{}, len(existing)) // set of existing seat coords
		for _, s := range existing {                    // fill set
			exists[key{r: strings.ToUpper(s.RowLabel), n: s.SeatNumber}] = struct{}{} // mark present
		}
		// collect seats to create
		var toCreate []repository.Seat         // batch insert payload
		for r := uint32(0); r < newRows; r++ { // iterate rows
			lbl := indexToRowLabel(int(r))          // compute row label
			for n := uint32(1); n <= newCols; n++ { // iterate seat numbers
				if strings.EqualFold(lbl, rowLabel) && n == seatNum { // skip the seat being created
					continue // don't duplicate
				}
				if _, ok := exists[key{r: lbl, n: n}]; ok { // already exists
					continue // skip
				}
				toCreate = append(toCreate, repository.Seat{ // add new
					HallID:     hall.ID,    // hall id
					RowLabel:   lbl,        // row label
					SeatNumber: n,          // seat number
					SeatType:   "STANDARD", // default type
				})
			}
		}
		if len(toCreate) > 0 { // if we have any to insert
			if err := h.SeatRepo.CreateBulk(c.Request().Context(), toCreate); err != nil { // bulk insert
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to backfill seats after expanding hall"}) // respond failure
			}
		}
	}
	// finally, create requested seat
	s := &repository.Seat{ // build seat record
		HallID:     hall.ID,  // hall ID
		RowLabel:   rowLabel, // normalized row label
		SeatNumber: seatNum,  // seat number
		SeatType:   seatType, // normalized type
		IsActive:   true,     // default active
	}
	if err := h.SeatRepo.Create(c.Request().Context(), s); err != nil { // attempt insert
		// handle duplicate seat conflict
		if strings.Contains(strings.ToLower(err.Error()), "1062") { // mysql duplicate entry
			return c.JSON(http.StatusConflict, map[string]string{"error": "seat already exists"}) // respond conflict
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "create failed"}) // generic failure
	}
	// read back populated row
	fresh, err := h.SeatRepo.GetByID(c.Request().Context(), s.ID) // fetch created seat
	if err != nil {                                               // fallback on read error
		return c.JSON(http.StatusCreated, s) // return created payload
	}
	return c.JSON(http.StatusCreated, fresh) // return fully populated seat
}

// UpdateSeat handles PUT/PATCH /v1/seats/:id and modifies seat attributes.
// - Expands hall grid if new row/number exceed current capacity
// - Prevents no-op updates (returns 409 when values are identical to DB)
// - Returns 409 on duplicate conflicts (MySQL 1062)
// - Enforces ownership through SeatRepo/HallRepo joins
func (h *OwnerHandler) UpdateSeat(c echo.Context) error { // begin UpdateSeat handler
	ownerID, err := getUserID(c) // retrieve user ID
	if err != nil {              // unauthorized when user ID is invalid
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // respond unauthorized
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64) // parse seat ID from path
	if err != nil {                                     // invalid seat ID
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"}) // respond invalid id
	}
	var body struct { // structure to bind JSON body
		RowLabel   string  `json:"row_label"`   // new row label
		SeatNumber uint32  `json:"seat_number"` // new seat number
		SeatType   *string `json:"seat_type"`   // optional new seat type
		IsActive   *bool   `json:"is_active"`   // optional active flag
	}
	if err := c.Bind(&body); err != nil { // bind incoming JSON
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"}) // respond bad request
	}
	rowLabel := normalizeRowLabel(body.RowLabel) // sanitize the row label
	if rowLabel == "" || body.SeatNumber == 0 {  // row label and seat number are mandatory
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "row_label and seat_number are required"}) // respond validation error
	}
	var normalizedType string // local variable for the normalized seat type
	if body.SeatType != nil { // seat type provided
		normalizedType = strings.ToUpper(strings.TrimSpace(*body.SeatType)) // normalize provided seat type
		switch normalizedType {                                             // validate allowed values
		case "", "STANDARD", "VIP", "ACCESSIBLE": // allowed values including empty which defaults to STANDARD
			if normalizedType == "" { // empty string defaults
				normalizedType = "STANDARD" // assign default seat type
			}
		case "DISABLED": // map DISABLED to ACCESSIBLE
			normalizedType = "ACCESSIBLE" // assign accessible seat type
		default: // any other string is invalid
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid seat type"}) // respond invalid seat type
		}
	}
	curSeat, err := h.SeatRepo.GetByIDAndOwner(c.Request().Context(), id, ownerID) // load the current seat to verify ownership
	if err != nil {                                                                // handle retrieval errors
		if err == repository.ErrSeatNotFound { // seat not found for owner
			return c.JSON(http.StatusNotFound, map[string]string{"error": "seat not found"}) // respond not found
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"}) // generic database error
	}
	isActive := curSeat.IsActive // start with current active flag
	if body.IsActive != nil {    // update when provided
		isActive = *body.IsActive // assign new value
	}

	// Guard: if nothing changed, avoid UPDATE and return 409 (no changes)
	sameType := (body.SeatType == nil) || strings.EqualFold(normalizedType, curSeat.SeatType)
	if strings.EqualFold(rowLabel, curSeat.RowLabel) &&
		body.SeatNumber == curSeat.SeatNumber &&
		sameType &&
		isActive == curSeat.IsActive {
		return c.JSON(http.StatusConflict, map[string]string{"error": "no changes"})
	}

	hall, err := h.HallRepo.GetByIDAndOwner(c.Request().Context(), curSeat.HallID, ownerID) // fetch the hall owning the seat
	if err != nil {                                                                         // handle hall retrieval errors
		if err == repository.ErrHallNotFound { // hall not found for owner
			return c.JSON(http.StatusNotFound, map[string]string{"error": "hall not found"}) // respond not found
		}
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"}) // generic database error
	}
	reqRowIdx, ok := rowLabelToIndex(rowLabel) // convert row label to index for expansion logic
	if !ok {                                   // invalid row label conversion
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid row_label"}) // respond invalid row label
	}
	curRows := uint32(0)     // current row count in hall
	curCols := uint32(0)     // current column count in hall
	if hall.SeatRows.Valid { // hall has a row count specified
		curRows = uint32(hall.SeatRows.Int32) // convert to uint32
	}
	if hall.SeatCols.Valid { // hall has a column count specified
		curCols = uint32(hall.SeatCols.Int32) // convert to uint32
	}
	needsExpand := uint32(reqRowIdx+1) > curRows || body.SeatNumber > curCols // determine if hall expansion is necessary
	if needsExpand {                                                          // perform hall expansion when seat moves beyond current layout
		newRows := curRows                 // start with current rows
		newCols := curCols                 // start with current cols
		if uint32(reqRowIdx+1) > newRows { // if requested row index exceeds current rows
			newRows = uint32(reqRowIdx + 1) // set new rows to accommodate
		}
		if body.SeatNumber > newCols { // if requested seat number exceeds current cols
			newCols = body.SeatNumber // set new cols accordingly
		}
		updHall := &repository.Hall{ // build hall update model
			ID:          hall.ID,                                           // hall ID
			OwnerID:     ownerID,                                           // owner ID
			CinemaID:    hall.CinemaID,                                     // preserve cinema ID
			Name:        hall.Name,                                         // preserve hall name
			Description: hall.Description,                                  // preserve description
			SeatRows:    sql.NullInt32{Int32: int32(newRows), Valid: true}, // update row count
			SeatCols:    sql.NullInt32{Int32: int32(newCols), Valid: true}, // update column count
			IsActive:    hall.IsActive,                                     // preserve active flag
			CreatedAt:   hall.CreatedAt,                                    // preserve creation time
			UpdatedAt:   hall.UpdatedAt,                                    // preserve update time
		}
		if err := h.HallRepo.UpdateByIDAndOwner(c.Request().Context(), updHall); err != nil { // persist hall expansion
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to expand hall capacity"}) // respond error on failure
		}
		// Backfill missing seats in expanded area
		existing, err := h.SeatRepo.GetByHall(c.Request().Context(), hall.ID) // read existing seats
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load seats"}) // respond failure
		}
		type key struct {
			r string
			n uint32
		} // composite key
		exists := make(map[key]struct{}, len(existing)) // set of existing
		for _, s := range existing {                    // mark existing seats
			exists[key{r: strings.ToUpper(s.RowLabel), n: s.SeatNumber}] = struct{}{}
		}
		toCreate := make([]repository.Seat, 0, int(newRows*newCols)) // slice to collect seats to create
		for r := uint32(0); r < newRows; r++ {                       // iterate rows
			lbl := indexToRowLabel(int(r))          // compute row label
			for n := uint32(1); n <= newCols; n++ { // iterate seat numbers
				if strings.EqualFold(lbl, rowLabel) && n == body.SeatNumber { // skip the seat being updated
					continue // do not backfill the new seat position
				}
				if _, ok := exists[key{r: lbl, n: n}]; ok { // skip seats that already exist
					continue // seat already present
				}
				toCreate = append(toCreate, repository.Seat{ // append a default seat record
					HallID:     hall.ID,    // hall ID
					RowLabel:   lbl,        // row label
					SeatNumber: n,          // seat number
					SeatType:   "STANDARD", // default seat type
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
			if err == repository.ErrNoChange {
				return c.JSON(http.StatusConflict, map[string]string{"error": "no changes"})
			}
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "update failed"}) // generic update error
		}
	} else { // updating without seat type
		if err := h.SeatRepo.UpdateByIDAndOwner(c.Request().Context(), id, ownerID, rowLabel, body.SeatNumber, isActive); err != nil { // update seat (no type)
			if err == sql.ErrNoRows { // seat not found or not owned
				return c.JSON(http.StatusNotFound, map[string]string{"error": "seat not found"}) // respond not found
			}
			if strings.Contains(err.Error(), "1062") { // duplicate seat position
				return c.JSON(http.StatusConflict, map[string]string{"error": "seat already exists"}) // respond conflict
			}
			if err == repository.ErrNoChange {
				return c.JSON(http.StatusConflict, map[string]string{"error": "no changes"})
			}
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "update failed"}) // generic error
		}
	}
	// fetch updated seat
	updated, err := h.SeatRepo.GetByIDAndOwner(c.Request().Context(), id, ownerID) // read updated seat
	if err != nil {                                                                // fallback on read error
		resp := map[string]any{ // minimal response
			"id":          id,              // seat id
			"hall_id":     curSeat.HallID,  // hall id
			"row_label":   rowLabel,        // row label
			"seat_number": body.SeatNumber, // seat number
			"seat_type": func() string { // type
				if body.SeatType != nil {
					return normalizedType
				}
				return curSeat.SeatType
			}(),
			"is_active": isActive, // active flag
		}
		return c.JSON(http.StatusOK, resp) // return minimal payload
	}
	return c.JSON(http.StatusOK, updated) // respond with updated seat
}

// DeleteSeat handles DELETE /v1/seats/:id and removes the specified seat.  Ownership is enforced via join with halls.
func (h *OwnerHandler) DeleteSeat(c echo.Context) error { // begin DeleteSeat handler
	ownerID, err := getUserID(c) // extract user ID from context
	if err != nil {              // user not authenticated
		return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // respond unauthorized
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64) // parse seat ID from path
	if err != nil {                                     // invalid seat ID provided
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
