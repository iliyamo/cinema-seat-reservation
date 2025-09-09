package handler // handler package contains owner-specific hall handlers

import (
    "database/sql"                                              // sql provides nullable types and error values
    "net/http"                                                 // http defines status code constants
    "strconv"                                                 // strconv parses URL parameters to numbers
    "strings"                                                 // strings manipulates and trims text
    "errors"                                                  // errors package for comparing sentinels

    "github.com/iliyamo/cinema-seat-reservation/internal/repository" // repository exposes database models
    "github.com/labstack/echo/v4"                                   // echo framework supplies request context
)

// CreateHall handles POST /v1/halls and creates a hall along with its initial seat layout
func (h *OwnerHandler) CreateHall(c echo.Context) error { // begin CreateHall handler
    ownerID, err := getUserID(c) // retrieve authenticated user ID
    if err != nil { // check authentication error
        return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // respond unauthorized when user ID is invalid
    }
    var body struct { // anonymous struct to bind JSON payload
        CinemaID    *uint64 `json:"cinema_id"`    // optional ID of the parent cinema
        Name        string  `json:"name"`         // required hall name
        Description *string `json:"description"`  // optional description
        SeatRows    *uint32 `json:"seat_rows"`    // number of seating rows
        SeatCols    *uint32 `json:"seat_cols"`    // number of seats per row
        Rows        *uint32 `json:"rows"`         // legacy alias for seat_rows
        Cols        *uint32 `json:"cols"`         // legacy alias for seat_cols
    }
    if err := c.Bind(&body); err != nil { // bind the incoming JSON
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"}) // respond bad request on binding errors
    }
    rowsPtr := body.SeatRows // seatRows may be nil
    if rowsPtr == nil { // fallback to legacy field when seatRows is absent
        rowsPtr = body.Rows // use legacy rows field
    }
    colsPtr := body.SeatCols // seatCols may be nil
    if colsPtr == nil { // fallback to legacy field when seatCols is absent
        colsPtr = body.Cols // use legacy cols field
    }
    if strings.TrimSpace(body.Name) == "" || rowsPtr == nil || colsPtr == nil || *rowsPtr == 0 || *colsPtr == 0 { // validate required fields
        return c.JSON(http.StatusBadRequest, map[string]string{ // respond with bad request when validation fails
            "error": "name, seat_rows and seat_cols are required and must be greater than zero", // descriptive error message
        })
    }
    var cinemaIDVal uint64 // hold resolved cinema ID
    if body.CinemaID != nil { // if a cinema ID was provided
        cinemaIDVal = *body.CinemaID // dereference the pointer
        if _, err := h.CinemaRepo.GetByIDAndOwner(c.Request().Context(), cinemaIDVal, ownerID); err != nil { // verify the cinema belongs to owner
            if err == repository.ErrCinemaNotFound { // not found error
                return c.JSON(http.StatusNotFound, map[string]string{"error": "cinema not found"}) // respond with not found
            }
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to verify cinema"}) // respond with internal error
        }
    }
    seatRows := int32(*rowsPtr) // convert row count to int32 for sql.NullInt32
    seatCols := int32(*colsPtr) // convert column count to int32
    var cinemaID *uint64 // pointer to cinemaID, nil when no cinema
    if body.CinemaID != nil { // assign pointer when provided
        cinemaID = &cinemaIDVal // reference the local variable
    }
    var desc sql.NullString // prepare description as nullable string
    if body.Description != nil && strings.TrimSpace(*body.Description) != "" { // description provided and not empty
        desc = sql.NullString{String: strings.TrimSpace(*body.Description), Valid: true} // assign valid description
    } else { // no description provided
        desc = sql.NullString{String: "", Valid: false} // set invalid description
    }
    hall := &repository.Hall{ // build a hall model
        OwnerID:     ownerID,                                              // assign owner ID
        CinemaID:    cinemaID,                                             // assign cinema ID pointer
        Name:        strings.TrimSpace(body.Name),                         // trimmed hall name
        Description: desc,                                                 // nullable description
        SeatRows:    sql.NullInt32{Int32: seatRows, Valid: true},          // number of rows stored as nullable int32
        SeatCols:    sql.NullInt32{Int32: seatCols, Valid: true},          // number of columns stored as nullable int32
    }
    // Before creating the hall, ensure no other hall exists with identical attributes
    if ok, err := h.HallRepo.ExistsExact(c.Request().Context(),
        ownerID, hall.CinemaID, hall.Name, hall.Description, hall.SeatRows, hall.SeatCols, nil); err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"})
    } else if ok {
        return c.JSON(http.StatusConflict, map[string]string{"error": "hall already exists with identical attributes"})
    }
    if err := h.HallRepo.Create(c.Request().Context(), hall); err != nil { // create hall in repository
        // Unexpected error occurred
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "could not create hall"})
    }
    total := int(*rowsPtr) * int(*colsPtr) // calculate total seats to preallocate slice
    seats := make([]repository.Seat, 0, total) // slice to hold seat definitions
    for rIdx := uint32(0); rIdx < *rowsPtr; rIdx++ { // iterate rows
        label := indexToRowLabel(int(rIdx)) // compute row label for index
        for cIdx := uint32(0); cIdx < *colsPtr; cIdx++ { // iterate columns
            seats = append(seats, repository.Seat{ // append a seat definition
                HallID:     hall.ID,           // assign hall ID
                RowLabel:   label,             // assign computed row label
                SeatNumber: cIdx + 1,          // seat numbers start from 1
                SeatType:   "STANDARD",       // default seat type
            })
        }
    }
    if err := h.SeatRepo.CreateBulk(c.Request().Context(), seats); err != nil { // insert all seats in bulk
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create seats"}) // respond with error on failure
    }
    return c.JSON(http.StatusCreated, hall) // return the created hall with created status
}

// UpdateHall handles PUT/PATCH /v1/halls/:id and updates hall properties.  When seat counts change it rebuilds the seat layout.
func (h *OwnerHandler) UpdateHall(c echo.Context) error { // begin UpdateHall handler
    ownerID, err := getUserID(c) // fetch user ID from context
    if err != nil { // unauthorized when user ID is invalid
        return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // respond unauthorized
    }
    id, err := strconv.ParseUint(c.Param("id"), 10, 64) // parse hall ID from path
    if err != nil { // ensure the hall ID is numeric
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"}) // invalid ID error
    }
    cur, err := h.HallRepo.GetByIDAndOwner(c.Request().Context(), id, ownerID) // load current hall to verify ownership
    if err != nil { // handle fetch error
        if err == repository.ErrHallNotFound { // hall not found for this owner
            return c.JSON(http.StatusNotFound, map[string]string{"error": "hall not found"}) // respond with not found
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"}) // generic database error
    }
    var body struct { // struct to bind JSON body
        Name        *string `json:"name"`        // optional new name
        Description *string `json:"description"` // optional new description
        SeatRows    *uint32 `json:"seat_rows"`   // optional new number of rows
        SeatCols    *uint32 `json:"seat_cols"`   // optional new number of columns
    }
    if err := c.Bind(&body); err != nil { // bind JSON payload
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"}) // respond bad request on binding error
    }
    name := cur.Name // start with current name
    // Update name when provided and non empty
    if body.Name != nil && strings.TrimSpace(*body.Name) != "" {
        name = strings.TrimSpace(*body.Name)
    }
    // Build description from the request or keep current
    desc := cur.Description
    if body.Description != nil {
        s := strings.TrimSpace(*body.Description)
        if s == "" {
            // an empty string should clear the description
            desc = sql.NullString{String: "", Valid: false}
        } else {
            desc = sql.NullString{String: s, Valid: true}
        }
    }
    // Determine new seat rows
    rows := cur.SeatRows
    if body.SeatRows != nil {
        if *body.SeatRows == 0 {
            return c.JSON(http.StatusBadRequest, map[string]string{"error": "seat_rows must be greater than zero"})
        }
        rows = sql.NullInt32{Int32: int32(*body.SeatRows), Valid: true}
    }
    // Determine new seat columns
    cols := cur.SeatCols

    // If no seat columns were provided in the body, cols remains the current value.
    if body.SeatCols != nil {
        if *body.SeatCols == 0 {
            return c.JSON(http.StatusBadRequest, map[string]string{"error": "seat_cols must be greater than zero"})
        }
        cols = sql.NullInt32{Int32: int32(*body.SeatCols), Valid: true}
    }
    // If all four attributes are unchanged, return a 409 Conflict: nothing to update
    sameName := name == cur.Name
    sameDesc := (desc.Valid == cur.Description.Valid) && (!desc.Valid || desc.String == cur.Description.String)
    sameRows := (rows.Valid == cur.SeatRows.Valid) && (!rows.Valid || rows.Int32 == cur.SeatRows.Int32)
    sameCols := (cols.Valid == cur.SeatCols.Valid) && (!cols.Valid || cols.Int32 == cur.SeatCols.Int32)
    if sameName && sameDesc && sameRows && sameCols {
        return c.JSON(http.StatusConflict, map[string]string{"error": "hall already has these parameters"})
    }
    // Check if another hall exists with identical attributes.  If so, return conflict.
    {
        if ok, err := h.HallRepo.ExistsExact(c.Request().Context(), ownerID, cur.CinemaID, name, desc, rows, cols, &id); err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"})
        } else if ok {
            return c.JSON(http.StatusConflict, map[string]string{"error": "hall name/rows/cols/desc already used by another hall"})
        }
    }
    // Determine whether the seat layout will change based on requested values.
    curRows := uint32(0)
    if cur.SeatRows.Valid {
        curRows = uint32(cur.SeatRows.Int32)
    }
    curCols := uint32(0)
    if cur.SeatCols.Valid {
        curCols = uint32(cur.SeatCols.Int32)
    }
    newRows := curRows
    newCols := curCols
    if rows.Valid {
        newRows = uint32(rows.Int32)
    }
    if cols.Valid {
        newCols = uint32(cols.Int32)
    }
    gridChanged := newRows != curRows || newCols != curCols
    if gridChanged {
        // Before rebuilding the seat grid, ensure there are no active reservations or holds
        // that reference seats in this hall.  If any exist, abort with a clear error.
        ctx := c.Request().Context()
        // Count seat holds referencing seats in this hall via seat_id join.
        var holdCount int
        if err := h.ShowRepo.DB().QueryRowContext(ctx,
            `SELECT COUNT(*) FROM seat_holds h JOIN seats s ON h.seat_id = s.id WHERE s.hall_id = ?`, id,
        ).Scan(&holdCount); err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"})
        }
        // Count reservation seats referencing seats in this hall via seat_id join.
        var resCount int
        if err := h.ShowRepo.DB().QueryRowContext(ctx,
            `SELECT COUNT(*) FROM reservation_seats rs JOIN seats s ON rs.seat_id = s.id WHERE s.hall_id = ?`, id,
        ).Scan(&resCount); err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"})
        }
        if holdCount > 0 || resCount > 0 {
            return c.JSON(http.StatusBadRequest, map[string]string{
                "error": "Cannot update seat grid: shows or reservations are using seats",
            })
        }

        // Rebuild the seat layout and associated show_seats in a single transaction.
        tx, err := h.ShowRepo.DB().BeginTx(ctx, nil)
        if err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to start transaction"})
        }
        committed := false
        defer func() {
            if !committed {
                _ = tx.Rollback()
            }
        }()

        // Update hall metadata inside the transaction.  At this point we know the grid will change.
        _, err = tx.ExecContext(ctx,
            `UPDATE halls SET name = ?, description = ?, seat_rows = ?, seat_cols = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND owner_id = ?`,
            name, desc, rows, cols, id, ownerID,
        )
        if err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update hall"})
        }

        // Remove all show_seats for shows in this hall before deleting seats to avoid FK violations.
        if _, err = tx.ExecContext(ctx,
            `DELETE ss FROM show_seats ss JOIN shows sh ON sh.id = ss.show_id WHERE sh.hall_id = ?`, id,
        ); err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to clear show_seats"})
        }

        // Delete old seats now that show_seats are cleared.
        if _, err = tx.ExecContext(ctx, `DELETE FROM seats WHERE hall_id = ?`, id); err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete old seats"})
        }

        // Insert new seat grid.  Ensure non-zero dimensions have been validated earlier.
        if newRows == 0 || newCols == 0 {
            return c.JSON(http.StatusBadRequest, map[string]string{"error": "seat_rows and seat_cols must be greater than zero"})
        }
        var sb strings.Builder
        sb.WriteString(`INSERT INTO seats (hall_id, row_label, seat_number, seat_type) VALUES `)
        args := make([]any, 0, int(newRows*newCols)*4)
        first := true
        for r := uint32(0); r < newRows; r++ {
            lbl := indexToRowLabel(int(r))
            for n := uint32(1); n <= newCols; n++ {
                if !first {
                    sb.WriteByte(',')
                } else {
                    first = false
                }
                sb.WriteString("(?, ?, ?, ?)")
                args = append(args, id, lbl, n, "STANDARD")
            }
        }
        if _, err = tx.ExecContext(ctx, sb.String(), args...); err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create new seats"})
        }

        // Fetch all shows for this hall to rebuild their show seats.
        showRows, err := tx.QueryContext(ctx, `SELECT id, base_price_cents FROM shows WHERE hall_id = ?`, id)
        if err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load shows"})
        }
        type showInfo struct {
            id    uint64
            price uint32
        }
        var shows []showInfo
        for showRows.Next() {
            var sid uint64
            var price uint32
            if err = showRows.Scan(&sid, &price); err != nil {
                showRows.Close()
                return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read show"})
            }
            shows = append(shows, showInfo{id: sid, price: price})
        }
        if err = showRows.Err(); err != nil {
            showRows.Close()
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load shows"})
        }
        showRows.Close()

        // Load the new seat IDs for this hall, ordered for consistent seat numbering.
        seatRows, err := tx.QueryContext(ctx, `SELECT id FROM seats WHERE hall_id = ? ORDER BY row_label, seat_number`, id)
        if err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load seats"})
        }
        var seatIDs []uint64
        for seatRows.Next() {
            var sid uint64
            if err = seatRows.Scan(&sid); err != nil {
                seatRows.Close()
                return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read seats"})
            }
            seatIDs = append(seatIDs, sid)
        }
        if err = seatRows.Err(); err != nil {
            seatRows.Close()
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load seats"})
        }
        seatRows.Close()

        // For each show, rebuild its show_seats using the new seats and base price.
        for _, sh := range shows {
            ss := make([]repository.ShowSeat, 0, len(seatIDs))
            for _, sid := range seatIDs {
                ss = append(ss, repository.ShowSeat{
                    ShowID:     sh.id,
                    SeatID:     sid,
                    Status:     "FREE",
                    PriceCents: sh.price,
                    Version:    1,
                })
            }
            if err = h.ShowSeatRepo.CreateBulkTx(ctx, tx, ss); err != nil {
                return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to rebuild show_seats"})
            }
        }

        if err = tx.Commit(); err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to commit transaction"})
        }
        committed = true

        // Reload and return the updated hall record.
        fresh, err := h.HallRepo.GetByID(ctx, id)
        if err != nil {
            // If retrieving the hall fails, still return the user-supplied fields.
            return c.JSON(http.StatusOK, map[string]any{
                "id":          id,
                "name":        name,
                "description": desc,
                "seat_rows":   rows,
                "seat_cols":   cols,
            })
        }
        return c.JSON(http.StatusOK, fresh)
    }
    // If the seat layout does not change, simply update the hall through the repository
    upd := &repository.Hall{
        ID:          id,
        OwnerID:     ownerID,
        CinemaID:    cur.CinemaID,
        Name:        name,
        Description: desc,
        SeatRows:    rows,
        SeatCols:    cols,
        IsActive:    cur.IsActive,
        CreatedAt:   cur.CreatedAt,
        UpdatedAt:   cur.UpdatedAt,
    }
    if err := h.HallRepo.UpdateByIDAndOwner(c.Request().Context(), upd); err != nil {
        if err == sql.ErrNoRows {
            return c.JSON(http.StatusNotFound, map[string]string{"error": "hall not found"})
        }
        if errors.Is(err, repository.ErrHallConflict) {
            return c.JSON(http.StatusConflict, map[string]string{"error": "hall name/rows/cols/desc already used by another hall"})
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "update failed"})
    }
    fresh, _ := h.HallRepo.GetByID(c.Request().Context(), id)
    return c.JSON(http.StatusOK, fresh)
}

// ListHallsInCinema handles GET /v1/cinemas/:cinema_id/halls and lists halls for a cinema owned by the user
func (h *OwnerHandler) ListHallsInCinema(c echo.Context) error { // begin ListHallsInCinema handler
    ownerID, err := getUserID(c) // extract user ID
    if err != nil { // invalid user ID
        return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // respond unauthorized
    }
    cinemaID, err := strconv.ParseUint(c.Param("cinema_id"), 10, 64) // parse cinema ID from path
    if err != nil { // invalid ID format
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid cinema_id"}) // respond bad request
    }
    if _, err := h.CinemaRepo.GetByIDAndOwner(c.Request().Context(), cinemaID, ownerID); err != nil { // verify ownership of cinema
        if err == repository.ErrCinemaNotFound { // cinema does not exist for this owner
            return c.JSON(http.StatusNotFound, map[string]string{"error": "cinema not found"}) // respond not found
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"}) // generic database error
    }
    items, err := h.HallRepo.ListByCinemaAndOwner(c.Request().Context(), cinemaID, ownerID) // list halls in the cinema
    if err != nil { // handle errors from repository
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"}) // respond with internal error
    }
    return c.JSON(http.StatusOK, map[string]any{"items": items}) // return halls list wrapped in JSON
}