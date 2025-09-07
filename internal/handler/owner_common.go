package handler // handler defines http handlers

import (
    "errors"       // errors provides sentinel values used in getUserID
    "strconv"      // strconv converts strings to numeric types
    "strings"      // strings provides trimming and case helpers

    "github.com/iliyamo/cinema-seat-reservation/internal/repository" // repository holds data access layer
    "github.com/labstack/echo/v4"                                    // echo defines request context types
)

// OwnerHandler bundles repositories for owners to manipulate resources
type OwnerHandler struct {
    CinemaRepo   *repository.CinemaRepo   // CinemaRepo provides cinema persistence
    HallRepo     *repository.HallRepo     // HallRepo provides hall persistence
    SeatRepo     *repository.SeatRepo     // SeatRepo provides seat persistence
    ShowRepo     *repository.ShowRepo     // ShowRepo provides show persistence
    ShowSeatRepo *repository.ShowSeatRepo // ShowSeatRepo provides show seat persistence
}

// NewOwnerHandler constructs a new OwnerHandler and panics if any dependency is nil
func NewOwnerHandler(cinemaRepo *repository.CinemaRepo, hallRepo *repository.HallRepo, seatRepo *repository.SeatRepo, showRepo *repository.ShowRepo, showSeatRepo *repository.ShowSeatRepo) *OwnerHandler { // create a new handler with its repositories
    if cinemaRepo == nil || hallRepo == nil || seatRepo == nil || showRepo == nil || showSeatRepo == nil { // check for nil dependencies
        panic("nil repository passed to NewOwnerHandler") // panic when a repository is missing
    }
    return &OwnerHandler{ // return a pointer to the new handler
        CinemaRepo:   cinemaRepo,   // assign cinema repository
        HallRepo:     hallRepo,     // assign hall repository
        SeatRepo:     seatRepo,     // assign seat repository
        ShowRepo:     showRepo,     // assign show repository
        ShowSeatRepo: showSeatRepo, // assign show seat repository
    }
}

// getUserID extracts the user_id from echo.Context and converts it to uint64
func getUserID(c echo.Context) (uint64, error) { // begin getUserID helper
    v := c.Get("user_id") // fetch user_id from context
    switch t := v.(type) { // perform type switch on the value
    case uint64: // when already uint64
        return t, nil // return directly
    case int: // when stored as int
        return uint64(t), nil // convert to uint64
    case int64: // when stored as int64
        return uint64(t), nil // convert to uint64
    case float64: // when stored as float64
        return uint64(t), nil // convert to uint64
    case string: // when stored as string
        if n, err := strconv.ParseUint(t, 10, 64); err == nil { // parse string to uint64
            return n, nil // return parsed number
        }
    } // end type switch
    return 0, errors.New("invalid user_id in context") // return error if value is missing or invalid
}

// indexToRowLabel converts a zero-based index to an alphabetical row label like A, B, AA
func indexToRowLabel(i int) string { // begin function to compute row label
    if i < 0 { // negative indices are invalid
        return "" // return empty string for invalid index
    }
    res := []rune{} // accumulate runes for the label
    for { // loop until all digits consumed
        rem := i % 26 // compute remainder in base 26
        res = append(res, rune('A'+rem)) // append current letter
        i = i/26 - 1 // reduce i for next digit
        if i < 0 { // break when no more digits
            break // exit loop
        }
    } // end for
    for j, k := 0, len(res)-1; j < k; j, k = j+1, k-1 { // reverse the runes to build the label
        res[j], res[k] = res[k], res[j] // swap positions
    }
    return string(res) // convert rune slice to string
}

// rowLabelToIndex converts a row label like A or AA into its zero-based index
func rowLabelToIndex(label string) (int, bool) { // begin function
    s := strings.ToUpper(strings.TrimSpace(label)) // normalize the label to upper case
    if s == "" { // empty label is invalid
        return -1, false // return false indicator
    }
    n := 0 // accumulator for numeric value
    for i := 0; i < len(s); i++ { // iterate over characters
        ch := s[i] // current byte
        if ch < 'A' || ch > 'Z' { // only ASCII A-Z are valid
            return -1, false // return invalid when encountering other letters
        }
        n = n*26 + int(ch-'A'+1) // accumulate base26 representation
    }
    return n - 1, true // return zero-based index and true
}

// normalizeRowLabel strips non ASCII letters and converts to uppercase
func normalizeRowLabel(raw string) string { // begin normalization helper
    var b strings.Builder // create builder for efficiency
    for _, r := range raw { // iterate over runes
        if r >= 'a' && r <= 'z' { // handle lowercase ASCII letters
            b.WriteRune(r - 32) // convert lowercase to uppercase
        } else if r >= 'A' && r <= 'Z' { // handle uppercase ASCII letters
            b.WriteRune(r) // append uppercase letter
        } // ignore all other characters
    } // end iteration
    return b.String() // return resulting string
}