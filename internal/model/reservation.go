package model

import "time"

// Reservation records a user's booking for a specific show.
// It aggregates one or more seats booked under a single
// transaction and tracks the overall status and total amount.
//
// Fields:
//  ID               – primary key identifier.
//  UserID           – user who made the reservation.
//  ShowID           – show being reserved.
//  Status           – state of the reservation (PENDING, CONFIRMED,
//                     CANCELLED).
//  TotalAmountCents – total price in cents for all seats.
//  PaymentRef       – external payment reference, if any.
//  CreatedAt        – creation timestamp.
//  UpdatedAt        – last update timestamp.
type Reservation struct {
    ID               uint64     // reservations.id
    UserID           uint64     // reservations.user_id
    ShowID           uint64     // reservations.show_id
    Status           string     // reservations.status
    TotalAmountCents uint32     // reservations.total_amount_cents
    PaymentRef       *string    // reservations.payment_ref (nullable)
    CreatedAt        time.Time  // reservations.created_at
    UpdatedAt        time.Time  // reservations.updated_at
}

// ReservationSeat links a reservation to individual seats for a
// show.  Each record represents a seat purchased in a
// reservation.  Together they form the full set of seats
// contained in the reservation.
//
// Fields:
//  ID            – primary key identifier.
//  ReservationID – reference to the reservation.
//  ShowID        – show in which the seat is booked.
//  SeatID        – seat that has been reserved.
//  PriceCents    – price for this seat in cents.
//  CreatedAt     – creation timestamp.
type ReservationSeat struct {
    ID            uint64    // reservation_seats.id
    ReservationID uint64    // reservation_seats.reservation_id
    ShowID        uint64    // reservation_seats.show_id
    SeatID        uint64    // reservation_seats.seat_id
    PriceCents    uint32    // reservation_seats.price_cents
    CreatedAt     time.Time // reservation_seats.created_at
}