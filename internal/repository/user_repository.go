package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/iliyamo/cinema-seat-reservation/internal/utils"
)

// User mirrors the 'users' table.
type User struct {
	ID           uint64
	Email        string
	PasswordHash string
	Role         string
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UserRepo struct{ DB *sql.DB }

func NewUserRepo(db *sql.DB) *UserRepo { return &UserRepo{DB: db} }

var ErrEmailExists = errors.New("email already exists")

// Create inserts user and returns its ID.
func (r *UserRepo) Create(ctx context.Context, email, password, role string, cost int) (uint64, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	hash, err := utils.HashPassword(password, cost)
	if err != nil {
		return 0, err
	}
	res, err := r.DB.ExecContext(ctx,
		"INSERT INTO users (email, password_hash, role) VALUES (?,?,?)",
		email, hash, role)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "1062") {
			return 0, ErrEmailExists
		}
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return uint64(id), nil
}

// GetByEmail fetches a user by normalized email.
func (r *UserRepo) GetByEmail(ctx context.Context, email string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u User
	err := r.DB.QueryRowContext(ctx,
		"SELECT id,email,password_hash,role,is_active,created_at,updated_at FROM users WHERE email=? LIMIT 1",
		email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// GetByID fetches a user by id.
func (r *UserRepo) GetByID(ctx context.Context, id uint64) (User, error) {
	var u User
	err := r.DB.QueryRowContext(ctx,
		"SELECT id,email,password_hash,role,is_active,created_at,updated_at FROM users WHERE id=? LIMIT 1",
		id).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}
