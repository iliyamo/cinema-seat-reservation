package middleware

import (
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
)

// JWTAuth validates Bearer access token and injects 'sub' and 'role' into context.
func JWTAuth(secret string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth := c.Request().Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "missing bearer token"})
			}
			raw := strings.TrimPrefix(auth, "Bearer ")

			tok, err := jwt.Parse(raw, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, echo.ErrUnauthorized
				}
				return []byte(secret), nil
			})
			if err != nil || !tok.Valid {
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid token"})
			}

			claims, ok := tok.Claims.(jwt.MapClaims)
			if !ok {
				return c.JSON(http.StatusUnauthorized, echo.Map{"error": "invalid claims"})
			}

			c.Set("user_id", claims["sub"])
			c.Set("role", claims["role"])
			return next(c)
		}
	}
}
