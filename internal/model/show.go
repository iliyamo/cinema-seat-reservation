package model

import "time"

// Show represents a scheduled screening of a movie in a particular
// hall.  It contains information about the movie title, start and
// end times, pricing and status.  Shows are linked to halls and
// may have many associated show_seats.
//
// Fields:
//  ID             – primary key identifier.
//  HallID         – hall where the show is taking place.
//  Title          – movie title or an external reference.
//  StartsAt       – when the show begins.
//  EndsAt         – when the show ends (must be after StartsAt).
//  BasePriceCents – default price in cents for seats without a
//                   specific override.
//  Status         – current state of the show (SCHEDULED, CANCELLED,
//                   FINISHED).
//  CreatedAt      – creation timestamp.
//  UpdatedAt      – last update timestamp.
type Show struct {
    ID             uint64    // shows.id
    HallID         uint64    // shows.hall_id
    Title          string    // shows.title
    StartsAt       time.Time // shows.starts_at
    EndsAt         time.Time // shows.ends_at
    BasePriceCents uint32    // shows.base_price_cents
    Status         string    // shows.status
    CreatedAt      time.Time // shows.created_at
    UpdatedAt      time.Time // shows.updated_at
}