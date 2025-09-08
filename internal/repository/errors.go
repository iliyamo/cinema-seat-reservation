// Package repository defines error types that are reused across multiple
// repositories. These sentinel values allow higher layers such as
// handlers to distinguish between different failure scenarios. For
// example, ErrForbidden indicates that the current user is not
// authorized to perform an operation on a resource owned by
// someone else, while ErrConflict signals that an operation
// cannot proceed due to existing dependent records (e.g. deleting
// a show with active reservations).
package repository

import "errors"

// ErrForbidden is returned when the caller attempts an operation
// on a resource they do not own. Handlers should translate this
// into an HTTP 403 response.
var ErrForbidden = errors.New("forbidden")

// ErrConflict is returned when a delete or update cannot be
// performed because of conflicting state, such as attempting to
// delete a show that still has reservations. Handlers should
// translate this into an HTTP 409 response.
var ErrConflict = errors.New("conflict")