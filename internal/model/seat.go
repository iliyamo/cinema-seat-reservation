package model

import "time"

// Seat describes a physical seat in a hall.  Seats are
// uniquely identified by their hall, row label and seat number.
// The seat_type indicates whether the seat is standard, VIP or
// accessible for disabled patrons.
//
// Fields:
//  ID         – primary key identifier.
//  HallID     – hall to which this seat belongs.
//  RowLabel   – letter or string designating the row.
//  SeatNumber – number of the seat within the row.
//  SeatType   – type of seat (STANDARD, VIP, ACCESSIBLE).
//  IsActive   – whether the seat is active.
//  CreatedAt  – creation timestamp.
//  UpdatedAt  – last update timestamp.
type Seat struct {
    ID         uint64    // seats.id
    HallID     uint64    // seats.hall_id
    RowLabel   string    // seats.row_label
    SeatNumber uint32    // seats.seat_number
    SeatType   string    // seats.seat_type
    IsActive   bool      // seats.is_active
    CreatedAt  time.Time // seats.created_at
    UpdatedAt  time.Time // seats.updated_at
}