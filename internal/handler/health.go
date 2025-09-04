package handler // Handler package

import (
	"net/http" // HTTP status codes

	"github.com/labstack/echo/v4" // Echo framework
)

func Health(c echo.Context) error { // Health handler
	return c.String(http.StatusOK, "ok") // Respond with "ok"
}
