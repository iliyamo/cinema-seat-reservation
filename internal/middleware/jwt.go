package middleware // declare the middleware package; contains reusable HTTP middleware functions

import (
    "net/http"               // HTTP status codes for responses
    "strings"               // string utilities for prefix checking and trimming

    "github.com/golang-jwt/jwt/v5" // JWT library for parsing and validating tokens
    "github.com/labstack/echo/v4"  // Echo framework used for defining middleware and handlers
)

// JWTAuth returns an Echo middleware that validates a Bearer access token and
// injects the token's subject and role claims into the request context.  The
// provided secret must match the one used when issuing tokens.  This
// middleware should wrap protected routes so that handlers can access
// authenticated user information via `c.Get("user_id")` and `c.Get("role")`.
func JWTAuth(secret string) echo.MiddlewareFunc {
    // The outer function returns a middleware function.  Echo executes this
    // once when registering the middleware.
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        // The returned handler is invoked for each incoming HTTP request.
        return func(c echo.Context) error {
            // Read the Authorization header.  A valid header should start
            // with "Bearer " followed by the JWT.  If it doesn't, respond
            // with 401 Unauthorized indicating that authentication is
            // required.
            auth := c.Request().Header.Get("Authorization")
            if !strings.HasPrefix(auth, "Bearer ") {
                return c.JSON(http.StatusUnauthorized, echo.Map{"error": "missing bearer token"})
            }
            // Remove the "Bearer " prefix to obtain the raw token string.
            raw := strings.TrimPrefix(auth, "Bearer ")

            // Parse the token using the HS256 signing method and our secret.
            // The callback provided to jwt.Parse supplies the signing key and
            // ensures that the algorithm matches what we expect.  If the
            // signing method differs, we reject the token by returning an
            // unauthorized error.
            tok, err := jwt.Parse(raw, func(t *jwt.Token) (interface{}, error) {
                // Type assert the signing method to HMAC; reject others.
                if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
                    return nil, echo.ErrUnauthorized
                }
                // Return the secret bytes used to sign the token.
                return []byte(secret), nil
            })
            // If parsing failed or the token is invalid, respond with 401.
            if err != nil || !tok.Valid {
                return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid token"})
            }

            // Extract the claims into a map for easy access.  If the
            // assertion fails, the claims are not in the expected format.
            claims, ok := tok.Claims.(jwt.MapClaims)
            if !ok {
                return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid claims"})
            }

            // Store the subject (user ID) and role claims in the context.
            // Handlers and downstream middleware can access these values via
            // c.Get().  We leave type assertions to downstream consumers.
            c.Set("user_id", claims["sub"])
            c.Set("role", claims["role"])
            // Call the next handler in the chain and return its result.
            return next(c)
        }
    }
}
