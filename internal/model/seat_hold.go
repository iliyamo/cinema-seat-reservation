package model

import "time"

// SeatHold represents a temporary hold on a seat during the
// checkout process.  Holds prevent concurrent reservations from
// grabbing the same seat while a user is in the process of
// purchasing.  Holds expire automatically at their expires_at
// timestamp.
//
// Fields:
//  ID        – primary key identifier.
//  UserID    – user who holds the seat (nullable for guests).
//  ShowID    – show for which the seat is held.
//  SeatID    – seat being held.
//  HoldToken – unique token returned to the client for reference.
//  ExpiresAt – when the hold expires.
//  CreatedAt – when the hold was created.
type SeatHold struct {
    ID        uint64     // seat_holds.id
    UserID    *uint64    // seat_holds.user_id (nullable)
    ShowID    uint64     // seat_holds.show_id
    SeatID    uint64     // seat_holds.seat_id
    HoldToken string     // seat_holds.hold_token
    ExpiresAt time.Time  // seat_holds.expires_at
    CreatedAt time.Time  // seat_holds.created_at
}