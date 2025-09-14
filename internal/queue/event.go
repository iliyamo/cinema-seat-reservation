// Package queue defines message payloads exchanged over the message broker.
package queue

// BookingConfirmedEvent is published when a reservation is successfully confirmed.
// It contains enough information for downstream consumers to log, notify, or
// trigger analytics without querying the primary database.
type BookingConfirmedEvent struct {
    ReservationID    uint64   `json:"reservation_id"`
    UserID           uint64   `json:"user_id"`
    ShowID           uint64   `json:"show_id"`
    CinemaID         uint64   `json:"cinema_id"`
    CinemaName       string   `json:"cinema_name"`
    HallID           uint64   `json:"hall_id"`
    HallName         string   `json:"hall_name"`
    MovieTitle       string   `json:"movie_title"`
    StartsAt         string   `json:"starts_at"`
    EndsAt           string   `json:"ends_at"`
    SeatLabels       []string `json:"seats"`
    TotalAmountCents uint32   `json:"total_amount_cents"`
    ConfirmedAt      string   `json:"confirmed_at"`
}