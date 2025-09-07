package model

import "time"

// User represents an application user record as stored in the
// `users` table. Each field corresponds to a column in the
// database. The json tags are omitted here because these structs
// are primarily used internally by the repository layer; handlers
// may define separate response types with appropriate JSON tags.
// A user has either a Role string or a foreign key RoleID
// referencing the roles table, depending on which migration
// strategy was applied.  Both fields are included to support
// either schema.
//
// Fields:
//  ID           – primary key identifier of the user.
//  Email        – unique email address.
//  PasswordHash – bcrypt hashed password.
//  Role         – name of the role (e.g. CUSTOMER or OWNER).
//  RoleID       – foreign key into the roles table (tinyint).  May be zero if unused.
//  IsActive     – whether the account is active.
//  CreatedAt    – timestamp of creation.
//  UpdatedAt    – timestamp of last update.
type User struct {
    ID           uint64    // users.id
    Email        string    // users.email
    PasswordHash string    // users.password_hash
    Role         string    // users.role (deprecated when using RoleID)
    RoleID       uint8     // users.role_id (references roles.id)
    IsActive     bool      // users.is_active
    CreatedAt    time.Time // users.created_at
    UpdatedAt    time.Time // users.updated_at
}

// Role represents a row in the `roles` table.  It maps a small
// integer ID to a role name.  When using the normalized roles
// schema, users reference this table via the RoleID field.
//
// Fields:
//  ID   – numeric identifier of the role.
//  Name – unique role name (e.g. CUSTOMER, OWNER).
type Role struct {
    ID   uint8  // roles.id
    Name string // roles.name
}

// RefreshToken models an entry in the `refresh_tokens` table.  Each
// refresh token belongs to a user and contains metadata for expiry
// and revocation.  The plain token is not stored; only its
// SHA‑256 hash.
//
// Fields:
//  ID        – primary key identifier.
//  UserID    – owner of the token.
//  TokenHash – SHA‑256 hex digest of the token value.
//  ExpiresAt – expiration timestamp of the token.
//  RevokedAt – when the token was revoked (null if still active).
//  CreatedAt – timestamp of creation.
type RefreshToken struct {
    ID        uint64     // refresh_tokens.id
    UserID    uint64     // refresh_tokens.user_id
    TokenHash string     // refresh_tokens.token_hash
    ExpiresAt time.Time  // refresh_tokens.expires_at
    RevokedAt *time.Time // refresh_tokens.revoked_at (nullable)
    CreatedAt time.Time  // refresh_tokens.created_at
}