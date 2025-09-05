package handler // declare the package name; contains HTTP handlers

import (
    "net/http"          // net/http provides status codes and response helpers

    "github.com/labstack/echo/v4" // echo is the web framework used for this project
)

// Health is a simple health‑check endpoint used by load balancers and
// monitoring systems to verify that the service is running.  It returns
// a plain text "ok" message with an HTTP 200 status code.
func Health(c echo.Context) error { // Health handler signature accepts an echo context and returns an error
    return c.String(http.StatusOK, "ok") // write "ok" with a 200 OK status; String writes plain text
}
