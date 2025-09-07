package repository // repository for show domain operations

import (
    "context"       // context for controlling query lifetime
    "database/sql"   // sql provides DB abstraction
    "errors"         // errors for sentinel definitions
)

// Show represents a scheduled screening of a movie in a particular hall.
// StartsAt and EndsAt define the schedule; BasePriceCents is the default
// price for seats unless overridden per seat.
type Show struct {
    ID             uint64 // ID is the primary key of the show
    HallID         uint64 // HallID references the hall where the show occurs
    Title          string // Title is the name of the movie or event
    StartsAt       string // StartsAt is the ISO timestamp when the show begins
    EndsAt         string // EndsAt is the ISO timestamp when the show ends
    BasePriceCents uint32 // BasePriceCents is the base price for a seat in cents
    Status         string // Status is the state of the show (SCHEDULED, CANCELLED, FINISHED)
    CreatedAt      string // CreatedAt records row creation time
    UpdatedAt      string // UpdatedAt records last update time
}

// ErrShowNotFound indicates that a show was not located in the DB.
var ErrShowNotFound = errors.New("show not found")

// ShowRepo manages persistence for shows.
type ShowRepo struct {
    db *sql.DB
}

// NewShowRepo constructs a ShowRepo with the given DB handle.
func NewShowRepo(db *sql.DB) *ShowRepo {
    return &ShowRepo{db: db}
}

// Create inserts a new show into the database and assigns the generated
// ID back to the show struct.  The caller must provide hall_id,
// title, starts_at and ends_at.  BasePriceCents can be optionally
// supplied; if zero the DB default of 0 will be used.  Status is
// implicitly SCHEDULED by the DB.
func (r *ShowRepo) Create(ctx context.Context, s *Show) error {
    const q = `INSERT INTO shows (hall_id, title, starts_at, ends_at, base_price_cents) VALUES (?, ?, ?, ?, ?)` // SQL insert for shows
    res, err := r.db.ExecContext(ctx, q, s.HallID, s.Title, s.StartsAt, s.EndsAt, s.BasePriceCents)            // execute insertion
    if err != nil {                                                                                           // check execution error
        return err                                                                                            // propagate the error
    }
    id, err := res.LastInsertId() // obtain the auto-incremented ID
    if err != nil {               // check error retrieving ID
        return err                // propagate error
    }
    s.ID = uint64(id) // assign the generated ID to the show model
    // Fetch the freshly inserted row to populate default fields (status, created_at, updated_at)
    const sel = `SELECT id, hall_id, title, starts_at, ends_at, base_price_cents, status, created_at, updated_at FROM shows WHERE id = ?` // select query
    err = r.db.QueryRowContext(ctx, sel, s.ID).Scan( // scan the selected row into the struct
        &s.ID, &s.HallID, &s.Title, &s.StartsAt, &s.EndsAt, &s.BasePriceCents, &s.Status, &s.CreatedAt, &s.UpdatedAt,
    )
    if err != nil { // check scanning error
        return err // propagate error
    }
    return nil // return nil on success
}

// GetByID retrieves a show by its ID.  It returns ErrShowNotFound if
// there is no matching row.
func (r *ShowRepo) GetByID(ctx context.Context, id uint64) (*Show, error) {
    const q = `SELECT id, hall_id, title, starts_at, ends_at, base_price_cents, status, created_at, updated_at FROM shows WHERE id = ?`
    var s Show
    err := r.db.QueryRowContext(ctx, q, id).Scan(&s.ID, &s.HallID, &s.Title, &s.StartsAt, &s.EndsAt, &s.BasePriceCents, &s.Status, &s.CreatedAt, &s.UpdatedAt)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, ErrShowNotFound
        }
        return nil, err
    }
    return &s, nil
}

// ListByHallAndOwner returns all shows for a given hall that belong to the specified owner.
// The owner constraint is enforced via the halls table.  Results are ordered by start
// time ascending.  When no shows exist it returns an empty slice and nil error.
func (r *ShowRepo) ListByHallAndOwner(ctx context.Context, hallID, ownerID uint64) ([]Show, error) {
    // Select shows joined with halls to check owner_id on halls.  Only select shows for
    // the requested hall and owner.
    const q = `SELECT s.id, s.hall_id, s.title, s.starts_at, s.ends_at, s.base_price_cents, s.status, s.created_at, s.updated_at
               FROM shows s
               JOIN halls h ON h.id = s.hall_id
               WHERE s.hall_id = ? AND h.owner_id = ?
               ORDER BY s.starts_at ASC`
    rows, err := r.db.QueryContext(ctx, q, hallID, ownerID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var result []Show
    for rows.Next() {
        var s Show
        if err := rows.Scan(
            &s.ID, &s.HallID, &s.Title, &s.StartsAt, &s.EndsAt, &s.BasePriceCents, &s.Status, &s.CreatedAt, &s.UpdatedAt,
        ); err != nil {
            return nil, err
        }
        result = append(result, s)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return result, nil
}

// FindOverlapping finds all shows in the specified hall whose scheduled time overlaps
// the provided interval [start, end).  A show overlaps when it starts before the
// proposed end and ends after the proposed start.  Time strings must use the same
// format as stored in the database ("2006-01-02 15:04:05").  It returns an empty
// slice when no overlaps are found.
func (r *ShowRepo) FindOverlapping(ctx context.Context, hallID uint64, start, end string) ([]Show, error) {
    // Use a predicate that selects shows where NOT (existing ends before new starts OR existing starts after new ends).
    const q = `SELECT id, hall_id, title, starts_at, ends_at, base_price_cents, status, created_at, updated_at
               FROM shows
               WHERE hall_id = ? AND NOT (ends_at <= ? OR starts_at >= ?)`
    rows, err := r.db.QueryContext(ctx, q, hallID, start, end)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var overlaps []Show
    for rows.Next() {
        var s Show
        if err := rows.Scan(
            &s.ID, &s.HallID, &s.Title, &s.StartsAt, &s.EndsAt, &s.BasePriceCents, &s.Status, &s.CreatedAt, &s.UpdatedAt,
        ); err != nil {
            return nil, err
        }
        overlaps = append(overlaps, s)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return overlaps, nil
}

// FindOverlappingExcluding is similar to FindOverlapping but excludes the show with the given ID
// from the overlap check.  This is used during updates to allow a show to overlap with itself.
func (r *ShowRepo) FindOverlappingExcluding(ctx context.Context, hallID, excludeID uint64, start, end string) ([]Show, error) {
    const q = `SELECT id, hall_id, title, starts_at, ends_at, base_price_cents, status, created_at, updated_at
               FROM shows
               WHERE hall_id = ? AND id <> ? AND NOT (ends_at <= ? OR starts_at >= ?)`
    rows, err := r.db.QueryContext(ctx, q, hallID, excludeID, start, end)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var overlaps []Show
    for rows.Next() {
        var s Show
        if err := rows.Scan(
            &s.ID, &s.HallID, &s.Title, &s.StartsAt, &s.EndsAt, &s.BasePriceCents, &s.Status, &s.CreatedAt, &s.UpdatedAt,
        ); err != nil {
            return nil, err
        }
        overlaps = append(overlaps, s)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return overlaps, nil
}

// UpdateByIDAndOwner updates a show's attributes if it belongs to a hall owned by the given owner.
// It returns sql.ErrNoRows when the show does not exist or the owner does not own the hall.
func (r *ShowRepo) UpdateByIDAndOwner(ctx context.Context, s *Show, ownerID uint64) error {
    const q = `UPDATE shows sh
               JOIN halls h ON h.id = sh.hall_id
               SET sh.title = ?, sh.starts_at = ?, sh.ends_at = ?, sh.base_price_cents = ?, sh.status = ?, sh.updated_at = CURRENT_TIMESTAMP
               WHERE sh.id = ? AND h.owner_id = ?`
    res, err := r.db.ExecContext(ctx, q, s.Title, s.StartsAt, s.EndsAt, s.BasePriceCents, s.Status, s.ID, ownerID)
    if err != nil {
        return err
    }
    if n, _ := res.RowsAffected(); n == 0 {
        return sql.ErrNoRows
    }
    return nil
}