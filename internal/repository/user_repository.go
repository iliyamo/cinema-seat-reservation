package repository // declare the repository package; contains data access logic

import (
	"context"      // context is used to provide cancellation and timeout to DB operations
	"database/sql" // standard library SQL package for interacting with databases
	"errors"       // errors for creating sentinel error values
	"strings"      // string helpers for normalization

	"github.com/iliyamo/cinema-seat-reservation/internal/model" // shared domain models
	"github.com/iliyamo/cinema-seat-reservation/internal/utils" // utilities such as password hashing
)

// NOTE: The User struct has been moved to the model package.  See
// internal/model/user.go for the full definition.  The repository
// returns and populates instances of model.User rather than
// redeclaring its own copy here.

// UserRepo provides methods for querying and modifying users.  It holds a
// pointer to a sql.DB which is shared across repositories.
type UserRepo struct{ DB *sql.DB }

// NewUserRepo constructs a new UserRepo given an open database handle.
func NewUserRepo(db *sql.DB) *UserRepo { return &UserRepo{DB: db} }

// ErrEmailExists indicates that an insert failed because the email already
// exists in the database.  Consumers can compare errors to this sentinel
// value to detect duplicate email errors.
var ErrEmailExists = errors.New("email already exists")

// Create inserts a new user into the database and returns the generated ID.
// It lowercases and trims the email, hashes the password using the provided
// bcrypt cost, and stores the given role.  If the insert fails due to a
// duplicate email (error containing "1062"), ErrEmailExists is returned.
func (r *UserRepo) Create(ctx context.Context, email, password, role string, cost int) (uint64, error) {
	// Normalize the email: trim spaces and convert to lower case.
	email = strings.ToLower(strings.TrimSpace(email))
	// Hash the plain password using the supplied bcrypt cost.  On error,
	// propagate the error up to the caller.
	hash, err := utils.HashPassword(password, cost)
	if err != nil {
		return 0, err
	}
	// Map the role name to the corresponding role ID.  If the role is
	// unrecognized default to CUSTOMER (1).  The mapping reflects the
	// seed values inserted into the roles table (see 0002_roles.up.sql).
	var roleID uint8
	switch strings.ToUpper(strings.TrimSpace(role)) {
	case "OWNER":
		roleID = 2
	case "CUSTOMER":
		fallthrough
	default:
		roleID = 1
	}
	// Execute the insert statement.  Use parameterized queries to avoid SQL injection.
	res, err := r.DB.ExecContext(ctx,
		"INSERT INTO users (email, password_hash, role_id) VALUES (?,?,?)",
		email, hash, roleID)
	if err != nil {
		// Check if the error message contains MySQL code 1062 (duplicate entry).  If so,
		// return the sentinel duplicate error.
		if strings.Contains(strings.ToLower(err.Error()), "1062") {
			return 0, ErrEmailExists
		}
		// Otherwise return the underlying error.
		return 0, err
	}
	// Retrieve the last insert ID from the result.  Some drivers return
	// int64; convert to uint64 for the ID field.
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return uint64(id), nil
}

// GetByEmail fetches a user by normalized email.  It normalizes the input
// email before querying.  Returns sql.ErrNoRows if the user is not found.
func (r *UserRepo) GetByEmail(ctx context.Context, email string) (model.User, error) {
	// Normalize the input email to lower case and trim spaces.
	email = strings.ToLower(strings.TrimSpace(email))
	var u model.User
	// Execute a parameterized SELECT joining the roles table.  We select
	// the role name via the join and the role_id field for completeness.
	err := r.DB.QueryRowContext(ctx,
		`SELECT u.id, u.email, u.password_hash, u.is_active, u.created_at, u.updated_at, u.role_id, r.name
         FROM users u
         LEFT JOIN roles r ON u.role_id = r.id
         WHERE u.email = ?
         LIMIT 1`,
		email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsActive, &u.CreatedAt, &u.UpdatedAt, &u.RoleID, &u.Role)
	return u, err
}

// GetByID fetches a user by its primary key.  Returns sql.ErrNoRows if not found.
func (r *UserRepo) GetByID(ctx context.Context, id uint64) (model.User, error) {
	var u model.User
	// Execute a parameterized SELECT joining roles.  See GetByEmail for details.
	err := r.DB.QueryRowContext(ctx,
		`SELECT u.id, u.email, u.password_hash, u.is_active, u.created_at, u.updated_at, u.role_id, r.name
         FROM users u
         LEFT JOIN roles r ON u.role_id = r.id
         WHERE u.id = ?
         LIMIT 1`,
		id).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsActive, &u.CreatedAt, &u.UpdatedAt, &u.RoleID, &u.Role)
	return u, err
}
