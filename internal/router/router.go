package router // Router package

import (
	"github.com/iliyamo/cinema-seat-reservation/internal/handler" // Import handlers
	"github.com/labstack/echo/v4"                                 // Echo framework
)

func RegisterRoutes(e *echo.Echo) {
	e.GET("/healthz", handler.Health) // GET /healthz route
}
