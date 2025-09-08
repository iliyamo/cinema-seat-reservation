package handler

import (
    "context"             // provides context with cancellation for DB calls
    "database/sql"         // SQL database interactions
    "net/http"             // HTTP status codes and primitives
    "strings"              // string manipulation utilities
    "time"                 // timeouts for DB calls

    "strconv"              // string-to-int conversion

    "github.com/golang-jwt/jwt/v5" // JSON Web Token library for parsing access tokens
    "github.com/labstack/echo/v4"  // Echo framework for HTTP routing

    "github.com/iliyamo/cinema-seat-reservation/internal/config"    // app configuration
    "github.com/iliyamo/cinema-seat-reservation/internal/repository" // DB repositories
    "github.com/iliyamo/cinema-seat-reservation/internal/utils"      // helper functions (hashing, token issuing)
)

// AuthHandler bundles dependencies for auth endpoints.
type AuthHandler struct {
	Cfg    config.Config
	Users  *repository.UserRepo
	Tokens *repository.TokenRepo
}

func NewAuthHandler(cfg config.Config, u *repository.UserRepo, t *repository.TokenRepo) *AuthHandler {
	return &AuthHandler{Cfg: cfg, Users: u, Tokens: t}
}

// ----- DTOs -----

type registerReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"` // CUSTOMER | OWNER
}
type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type refreshReq struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenPart struct {
	Token   string    `json:"token"`
	Expires time.Time `json:"expires"`
}
type userPart struct {
	ID    uint64 `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}
type authResp struct {
	User    userPart  `json:"user"`
	Access  tokenPart `json:"access"`
	Refresh tokenPart `json:"refresh"`
}

// Register: create user and return tokens immediately.
func (h *AuthHandler) Register(c echo.Context) error {
	var req registerReq
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid body"})
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" || req.Password == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "email/password required"})
	}
	role := strings.ToUpper(strings.TrimSpace(req.Role))
	if role != "OWNER" && role != "CUSTOMER" {
		role = "CUSTOMER"
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Second)
	defer cancel()

	uid, err := h.Users.Create(ctx, req.Email, req.Password, role, h.Cfg.BcryptCost)
	if err != nil {
		if err == repository.ErrEmailExists {
			return c.JSON(http.StatusConflict, echo.Map{"error": "email already exists"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "create user failed"})
	}

	access, err := utils.NewAccessToken(h.Cfg.JWTSecret, uid, role, h.Cfg.AccessTTLMin)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "issue access failed"})
	}
	refresh, err := utils.NewRefreshToken(h.Cfg.RefreshTTLDays)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "issue refresh failed"})
	}
	if err := h.Tokens.StoreRefresh(ctx, uid, utils.HashRefreshRaw(refresh.Raw), refresh.Exp); err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "save refresh failed"})
	}

	return c.JSON(http.StatusCreated, authResp{
		User:    userPart{ID: uid, Email: req.Email, Role: role},
		Access:  tokenPart{Token: access.Token, Expires: access.Exp},
		Refresh: tokenPart{Token: refresh.Raw, Expires: refresh.Exp}, // raw back to client
	})
}

// Login: verify and return new pair.
func (h *AuthHandler) Login(c echo.Context) error {
	var req loginReq
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid body"})
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" || req.Password == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "email/password required"})
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Second)
	defer cancel()

	u, err := h.Users.GetByEmail(ctx, req.Email)
	if err != nil {
		if err == sql.ErrNoRows {
			return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid credentials"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "query failed"})
	}
	if !utils.VerifyPassword(u.PasswordHash, req.Password) {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid credentials"})
	}

	access, err := utils.NewAccessToken(h.Cfg.JWTSecret, u.ID, u.Role, h.Cfg.AccessTTLMin)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "issue access failed"})
	}
	refresh, err := utils.NewRefreshToken(h.Cfg.RefreshTTLDays)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "issue refresh failed"})
	}
	if err := h.Tokens.StoreRefresh(ctx, u.ID, utils.HashRefreshRaw(refresh.Raw), refresh.Exp); err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "save refresh failed"})
	}

	return c.JSON(http.StatusOK, authResp{
		User:    userPart{ID: u.ID, Email: u.Email, Role: u.Role},
		Access:  tokenPart{Token: access.Token, Expires: access.Exp},
		Refresh: tokenPart{Token: refresh.Raw, Expires: refresh.Exp},
	})
}

// Refresh: validate by hash, revoke old, issue new.
func (h *AuthHandler) Refresh(c echo.Context) error {
	var req refreshReq
	if err := c.Bind(&req); err != nil || strings.TrimSpace(req.RefreshToken) == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "refresh_token required"})
	}
	raw := strings.TrimSpace(req.RefreshToken)
	hash := utils.HashRefreshRaw(raw)

	ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Second)
	defer cancel()

	userID, err := h.Tokens.ValidateRefresh(ctx, hash)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid refresh"})
	}
	_ = h.Tokens.RevokeByHash(ctx, hash)

	u, err := h.Users.GetByID(ctx, userID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "load user failed"})
	}

	access, err := utils.NewAccessToken(h.Cfg.JWTSecret, userID, u.Role, h.Cfg.AccessTTLMin)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "issue access failed"})
	}
	newRef, err := utils.NewRefreshToken(h.Cfg.RefreshTTLDays)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "issue refresh failed"})
	}
	if err := h.Tokens.StoreRefresh(ctx, userID, utils.HashRefreshRaw(newRef.Raw), newRef.Exp); err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "save refresh failed"})
	}

	return c.JSON(http.StatusOK, authResp{
		User:    userPart{ID: userID, Email: u.Email, Role: u.Role},
		Access:  tokenPart{Token: access.Token, Expires: access.Exp},
		Refresh: tokenPart{Token: newRef.Raw, Expires: newRef.Exp},
	})
}

// RefreshAccess: validate a refresh token and return a new access token WITHOUT rotating the refresh token.
// This endpoint can be used to obtain a fresh short-lived access token while reusing an existing refresh token.
func (h *AuthHandler) RefreshAccess(c echo.Context) error {
    var req refreshReq
    if err := c.Bind(&req); err != nil || strings.TrimSpace(req.RefreshToken) == "" {
        return c.JSON(http.StatusBadRequest, echo.Map{"error": "refresh_token required"})
    }
    raw := strings.TrimSpace(req.RefreshToken)
    hash := utils.HashRefreshRaw(raw)

    ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Second)
    defer cancel()

    userID, err := h.Tokens.ValidateRefresh(ctx, hash)
    if err != nil {
        // Invalid, expired or revoked refresh token
        return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid refresh"})
    }
    u, err := h.Users.GetByID(ctx, userID)
    if err != nil {
        if err == sql.ErrNoRows {
            return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid refresh"})
        }
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "load user failed"})
    }
    access, err := utils.NewAccessToken(h.Cfg.JWTSecret, userID, u.Role, h.Cfg.AccessTTLMin)
    if err != nil {
        return c.JSON(http.StatusInternalServerError, echo.Map{"error": "issue access failed"})
    }
    // Only return a new access token; do not rotate the refresh token
    return c.JSON(http.StatusOK, echo.Map{
        "access": tokenPart{Token: access.Token, Expires: access.Exp},
    })
}

// Logout: revoke all refresh tokens for current user (protected).
func (h *AuthHandler) Logout(c echo.Context) error {
    // The logout handler now supports two modes of operation: revoking a
    // specific refresh token or revoking all refresh tokens for the current
    // user.  If a valid JWT access token is provided in the Authorization
    // header and no refresh token is present in the body, the handler will
    // revoke all tokens for that user.  If a `refresh_token` is provided in
    // the request body, that particular token will be validated and then
    // revoked.  If neither is present, the request will be rejected.

    // First, inspect the Authorization header to see if the client supplied
    // a Bearer token.  Parsing the header here allows this endpoint to be
    // called without the JWT middleware.
    var uid uint64              // will hold the user ID extracted from JWT
    hasBearer := false          // flag indicating whether a valid bearer was found
    authHeader := c.Request().Header.Get("Authorization")
    // Only proceed if the header begins with "Bearer ".  We do not enforce
    // that a bearer must be present; absence simply means we will not be
    // revoking all sessions.
    if strings.HasPrefix(authHeader, "Bearer ") {
        // Trim the "Bearer " prefix to get the raw token value.
        rawToken := strings.TrimPrefix(authHeader, "Bearer ")
        // Parse the JWT using the configured secret.  Use the same signing
        // method (HMAC) that was used when generating the token.  If the
        // signature algorithm does not match, the parser will reject the
        // token.  This callback is required by the jwt library.
        tok, err := jwt.Parse(rawToken, func(t *jwt.Token) (interface{}, error) {
            // Ensure the signing method is what we expect.  Reject tokens
            // using different algorithms.
            if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
                return nil, echo.ErrUnauthorized
            }
            return []byte(h.Cfg.JWTSecret), nil
        })
        // If parsing succeeded and the token is valid, extract the `sub`
        // (subject) claim which contains the user ID and set the flag.
        if err == nil && tok.Valid {
            if claims, ok := tok.Claims.(jwt.MapClaims); ok {
                switch subVal := claims["sub"].(type) {
                case float64:
                    // JWT numeric values are decoded as float64; convert to
                    // uint64 for our user ID type.
                    uid = uint64(subVal)
                    hasBearer = true
                case string:
                    // Some token libraries encode numeric strings; attempt to
                    // parse the string as an unsigned integer.
                    if parsed, err := strconv.ParseUint(subVal, 10, 64); err == nil {
                        uid = parsed
                        hasBearer = true
                    }
                }
            }
        }
    }

    // Next, attempt to bind the JSON body to look for a refresh token.  If
    // the client sends invalid JSON it will simply leave req.RefreshToken
    // empty.  We do not immediately return an error here because the
    // Authorization header might suffice for revoking all tokens.
    var req refreshReq
    _ = c.Bind(&req)
    refreshToken := strings.TrimSpace(req.RefreshToken)

    // Create a context with timeout to bound the duration of database calls.
    ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Second)
    defer cancel()

    // If a valid bearer token was supplied and no refresh token is present
    // in the body, revoke all refresh tokens belonging to the user.  This
    // operation logs the user out of all sessions across devices.
    if hasBearer && refreshToken == "" {
        // Ensure we extracted a non‑zero user ID; if not, treat as
        // unauthorized.
        if uid == 0 {
            return c.JSON(http.StatusUnauthorized, echo.Map{"error": "unauthorized"})
        }
        // Revoke all active tokens for the user.  Any error indicates a
        // server problem and will be reported as HTTP 500.
        if err := h.Tokens.RevokeAllForUser(ctx, uid); err != nil {
            return c.JSON(http.StatusInternalServerError, echo.Map{"error": "logout failed"})
        }
        // Indicate success with no content.
        return c.NoContent(http.StatusNoContent)
    }
    // If a refresh token was provided, validate and revoke that specific
    // token.  A refresh token can always be used to log out a single
    // session even when no access token is available.
    if refreshToken != "" {
        // Compute the hash of the provided token; refresh tokens are stored
        // hashed in the database for security reasons.
        hash := utils.HashRefreshRaw(refreshToken)
        // Check that the hashed token exists and is not expired or revoked.
        if _, err := h.Tokens.ValidateRefresh(ctx, hash); err != nil {
            return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid refresh token"})
        }
        // Mark the specific token as revoked.  On failure, report a 500 error.
        if err := h.Tokens.RevokeByHash(ctx, hash); err != nil {
            return c.JSON(http.StatusInternalServerError, echo.Map{"error": "logout failed"})
        }
        // Successfully logged out this session.
        return c.NoContent(http.StatusNoContent)
    }
    // If neither a bearer token nor a refresh token were provided, the client
    // has not supplied enough information to perform a logout operation.
    return c.JSON(http.StatusBadRequest, echo.Map{"error": "provide Authorization header or refresh_token"})
}

// Me: simple protected endpoint.
func (h *AuthHandler) Me(c echo.Context) error {
	return c.JSON(http.StatusOK, echo.Map{
		"user_id": c.Get("user_id"),
		"role":    c.Get("role"),
	})
}
