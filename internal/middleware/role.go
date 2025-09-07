package middleware // middleware provides shared request processing for handlers

import (
    "net/http" // http package defines standard HTTP status codes
    "github.com/labstack/echo/v4" // echo provides middleware chaining and context
)

// RequireRole returns a middleware function that enforces that the
// authenticated user has one of the specified roles.  The roles
// accepted should correspond to the values stored in the JWT's "role"
// claim.  If the user's role is not in the allowed set, the request
// is aborted with a 403 Forbidden response.  It assumes a previous
// middleware has extracted the role into the context under the key
// "role".
func RequireRole(roles ...string) echo.MiddlewareFunc {
    // Build a set of allowed roles for constant‑time lookups.  The map
    // value is a boolean and is always true when present.
    allowed := make(map[string]bool, len(roles))
    for _, r := range roles {
        allowed[r] = true
    }
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            // Retrieve the role from context.  It should have been
            // stored by JWTAuth middleware as a string.  If not
            // present or of wrong type, treat as missing.
            v := c.Get("role")
            role, ok := v.(string)
            if !ok || !allowed[role] {
                // If role is missing or not allowed, return 403
                return c.JSON(http.StatusForbidden, map[string]string{"error": "forbidden"})
            }
            // Otherwise call the next handler in the chain
            return next(c)
        }
    }
}