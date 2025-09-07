package handler // handler package contains seat listing handlers

import (
    "net/http"                                                // http defines status code constants
    "sort"                                                   // sort provides sorting helpers
    "strconv"                                                // strconv converts URL parameters to numbers
    "strings"                                                // strings manipulates text and case

    "github.com/iliyamo/cinema-seat-reservation/internal/repository" // repository defines data models
    "github.com/labstack/echo/v4"                                   // echo provides request context and JSON helpers
)

// ListSeatsFlat handles GET /v1/halls/:hall_id/seats and returns a flat list of seats optionally filtered by active status
func (h *OwnerHandler) ListSeatsFlat(c echo.Context) error { // begin ListSeatsFlat handler
    ownerID, err := getUserID(c) // extract user ID
    if err != nil { // unauthorized when user ID is missing
        return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // respond unauthorized
    }
    hallID, err := strconv.ParseUint(c.Param("hall_id"), 10, 64) // parse hall ID from path
    if err != nil || hallID == 0 { // hall ID must be numeric and non zero
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid hall_id"}) // respond invalid parameter
    }
    // verify hall ownership
    if _, err := h.HallRepo.GetByIDAndOwner(c.Request().Context(), hallID, ownerID); err != nil { // check hall existence for owner
        if err == repository.ErrHallNotFound { // hall not found
            return c.JSON(http.StatusNotFound, map[string]string{"error": "hall not found"}) // respond not found
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"}) // generic DB error
    }
    seats, err := h.SeatRepo.GetByHall(c.Request().Context(), hallID) // retrieve all seats for the hall
    if err != nil { // handle retrieval error
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"}) // respond with DB error
    }
    // optional filtering by active status
    if v := strings.ToLower(strings.TrimSpace(c.QueryParam("active"))); v == "true" || v == "1" || v == "false" || v == "0" { // check presence of active query param
        want := v == "true" || v == "1" // desired active value
        filtered := make([]repository.Seat, 0, len(seats)) // slice to hold filtered seats
        for _, s := range seats { // iterate seats
            if s.IsActive == want { // include seat when it matches filter
                filtered = append(filtered, s) // append matching seat
            }
        }
        seats = filtered // replace original slice with filtered seats
    }
    // sort seats by ID ascending
    sort.Slice(seats, func(i, j int) bool { return seats[i].ID < seats[j].ID }) // apply custom sort by ID
    return c.JSON(http.StatusOK, map[string]any{ // respond with JSON object
        "hall_id": hallID, // include hall ID in response
        "count":   len(seats), // include total count after filtering
        "items":   seats, // include the seat list
    })
}

// ListSeatsLayout handles GET /v1/halls/:hall_id/seats/layout and returns a grouped view of seats per row
func (h *OwnerHandler) ListSeatsLayout(c echo.Context) error { // begin ListSeatsLayout handler
    ownerID, err := getUserID(c) // extract user ID
    if err != nil { // unauthorized when user ID is missing
        return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"}) // respond unauthorized
    }
    hallID, err := strconv.ParseUint(c.Param("hall_id"), 10, 64) // parse hall ID
    if err != nil || hallID == 0 { // hall ID must be valid
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid hall_id"}) // respond invalid parameter
    }
    // verify hall existence and ownership
    if _, err := h.HallRepo.GetByIDAndOwner(c.Request().Context(), hallID, ownerID); err != nil { // load hall for owner
        if err == repository.ErrHallNotFound { // hall missing
            return c.JSON(http.StatusNotFound, map[string]string{"error": "hall not found"}) // respond not found
        }
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"}) // generic DB error
    }
    // retrieve seats
    seats, err := h.SeatRepo.GetByHall(c.Request().Context(), hallID) // fetch all seats in the hall
    if err != nil { // handle DB error
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "db error"}) // respond DB error
    }
    // optional filtering of seats by active status
    if v := strings.ToLower(strings.TrimSpace(c.QueryParam("active"))); v == "true" || v == "1" || v == "false" || v == "0" { // active param recognized
        want := v == "true" || v == "1" // determine desired active state
        filtered := make([]repository.Seat, 0, len(seats)) // prepare slice for filtered seats
        for _, s := range seats { // iterate seats
            if s.IsActive == want { // include seat if active flag matches
                filtered = append(filtered, s) // add to filtered list
            }
        }
        seats = filtered // replace seats with filtered list
    }
    // group seats by row label and determine maximum column number
    rowsMap := make(map[string][]uint32) // map from uppercase row label to seat numbers
    maxCols := 0                         // track the largest seat number encountered
    for _, s := range seats { // iterate seats
        lbl := strings.ToUpper(strings.TrimSpace(s.RowLabel)) // normalize row label to uppercase
        rowsMap[lbl] = append(rowsMap[lbl], s.SeatNumber)     // append seat number to the row
        if int(s.SeatNumber) > maxCols {                      // update maxCols when this seat number is larger
            maxCols = int(s.SeatNumber) // assign new maximum
        }
    }
    // order the row labels alphabetically based on their index conversion
    rowOrder := make([]string, 0, len(rowsMap)) // slice to hold ordered row labels
    for lbl := range rowsMap { // collect keys from the map
        rowOrder = append(rowOrder, lbl) // append each label
    }
    sort.Slice(rowOrder, func(i, j int) bool { // sort the labels
        ii, okI := rowLabelToIndex(rowOrder[i]) // convert first label to index
        jj, okJ := rowLabelToIndex(rowOrder[j]) // convert second label to index
        if !okI || !okJ { // when conversion fails fall back to lexical comparison
            return rowOrder[i] < rowOrder[j] // compare strings
        }
        return ii < jj // compare numeric indices
    })
    // build compressed output with numbers and pretty strings
    type rowOut struct { // define output structure for each row
        RowLabel string   `json:"row_label"` // row label
        Numbers  []uint32 `json:"numbers"`  // sorted seat numbers
    }
    rowsOut := make([]rowOut, 0, len(rowOrder)) // slice to hold structured rows
    pretty := make([]string, 0, len(rowOrder))   // slice to hold human readable strings
    for _, lbl := range rowOrder { // iterate row labels in order
        nums := rowsMap[lbl]                   // get seat numbers for the row
        sort.Slice(nums, func(i, j int) bool { // sort seat numbers ascending
            return nums[i] < nums[j] // numeric comparison
        })
        rowsOut = append(rowsOut, rowOut{ // append structured row
            RowLabel: lbl, // assign row label
            Numbers:  nums, // assign sorted numbers
        })
        // build pretty string like "A: 1, 2, 3"
        var b strings.Builder           // builder for efficient string concatenation
        b.WriteString(lbl)              // write row label
        b.WriteString(": ")            // write separator
        for i, n := range nums {        // iterate sorted numbers
            if i > 0 {                  // add comma separator after first
                b.WriteString(", ") // write comma and space
            }
            b.WriteString(strconv.FormatUint(uint64(n), 10)) // append the seat number
        }
        pretty = append(pretty, b.String()) // append human readable row string
    }
    return c.JSON(http.StatusOK, map[string]any{ // respond with structured layout
        "hall_id":  hallID,    // include hall ID
        "max_cols": maxCols,   // include maximum column number
        "order":    rowOrder,  // include the order of row labels
        "rows":     rowsOut,   // include structured rows
        "pretty":   pretty,    // include pretty strings for display
    })
}