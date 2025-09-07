package model

import "time"

// ShowSeat links a seat to a particular show and tracks
// availability, pricing and versioning.  There is one show_seat
// record for every seat in a hall when a show is created.
//
// Fields:
//  ID         – primary key identifier.
//  ShowID     – the show to which this seat belongs.
//  SeatID     – the seat being made available.
//  Status     – availability status (FREE, HELD, RESERVED).
//  PriceCents – price in cents for this particular seat.
//  Version    – optimistic locking field to handle concurrent
//               updates.
//  CreatedAt  – timestamp when the record was created.
//  UpdatedAt  – timestamp when the record was last updated.
type ShowSeat struct {
    ID         uint64    // show_seats.id
    ShowID     uint64    // show_seats.show_id
    SeatID     uint64    // show_seats.seat_id
    Status     string    // show_seats.status
    PriceCents uint32    // show_seats.price_cents
    Version    uint32    // show_seats.version
    CreatedAt  time.Time // show_seats.created_at
    UpdatedAt  time.Time // show_seats.updated_at
}