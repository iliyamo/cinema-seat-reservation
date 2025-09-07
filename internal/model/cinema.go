package model

import "time"

// Cinema represents a movie theatre venue owned by a user.
// A cinema can contain multiple halls.  Each cinema belongs
// to one owner.  This struct corresponds to a row in the
// `cinemas` table.
//
// Fields:
//  ID        – primary key identifier.
//  OwnerID   – user ID of the cinema owner.
//  Name      – unique name of the cinema per owner.
//  CreatedAt – timestamp when the cinema was created.
//  UpdatedAt – timestamp of last update.
type Cinema struct {
    ID        uint64    // cinemas.id
    OwnerID   uint64    // cinemas.owner_id
    Name      string    // cinemas.name
    CreatedAt time.Time // cinemas.created_at
    UpdatedAt time.Time // cinemas.updated_at
}