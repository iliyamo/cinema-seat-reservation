package model

import "time"

// Hall represents an individual screening hall within a cinema.
// Halls belong to an owner and optionally to a cinema when
// using the cinemas migration.  Each hall has a unique name per
// owner and may define its seating layout via seatRows and seatCols.
//
// Fields:
//  ID          – primary key identifier.
//  OwnerID     – user ID of the hall owner.
//  CinemaID    – ID of the containing cinema (nil if not assigned).
//  Name        – unique hall name per owner.
//  Description – optional description of the hall.
//  SeatRows    – number of seating rows (nil if unspecified).
//  SeatCols    – number of seats per row (nil if unspecified).
//  IsActive    – whether the hall is active.
//  CreatedAt   – creation timestamp.
//  UpdatedAt   – last update timestamp.
type Hall struct {
    ID          uint64     // halls.id
    OwnerID     uint64     // halls.owner_id
    CinemaID    *uint64    // halls.cinema_id (nullable)
    Name        string     // halls.name
    Description *string    // halls.description (nullable)
    // SeatRows captures the number of seating rows in the hall.  This
    // field corresponds to the `seat_rows` column in the database.  A
    // pointer is used so that nil represents an unspecified value.
    SeatRows    *uint32    // halls.seat_rows (nullable)
    // SeatCols captures the number of seats per row in the hall.  This
    // field corresponds to the `seat_cols` column in the database.  A
    // pointer is used so that nil represents an unspecified value.
    SeatCols    *uint32    // halls.seat_cols (nullable)
    IsActive    bool       // halls.is_active
    CreatedAt   time.Time  // halls.created_at
    UpdatedAt   time.Time  // halls.updated_at
}