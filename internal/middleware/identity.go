package middleware

// identity.go defines helper functions shared across middleware files. Currently
// it provides a userID extraction function that pulls the subject (sub) or
// user_id claim from the JWT stored in the Echo context. When no token is
// present or no relevant claim exists, "guest" is returned.

import (
    "github.com/golang-jwt/jwt/v5"
    "github.com/labstack/echo/v4"
)

// userID extracts a user identifier from the JWT stored in context. It
// returns "guest" when no user is authenticated or the claims are missing.
func userID(c echo.Context) string {
    u := c.Get("user")
    if u == nil {
        return "guest"
    }
    if tok, ok := u.(*jwt.Token); ok {
        if cl, ok := tok.Claims.(jwt.MapClaims); ok {
            if v, ok := cl["sub"].(string); ok && v != "" {
                return v
            }
            if v, ok := cl["user_id"].(string); ok && v != "" {
                return v
            }
        }
    }
    return "guest"
}