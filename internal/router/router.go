package router // package router defines how HTTP routes are registered for the API

import (
    "github.com/labstack/echo/v4"                 // import the Echo web framework to handle routing

    "github.com/iliyamo/cinema-seat-reservation/internal/handler"   // import the handlers that implement business logic
    "github.com/iliyamo/cinema-seat-reservation/internal/middleware" // import middleware for JWT authentication
)

// RegisterRoutes registers non-authenticated routes on the provided Echo instance.
// At the moment it only exposes a health check endpoint.
// RegisterRoutes registers routes that do not require authentication on the
// provided Echo instance.  Currently it exposes only a health check.
func RegisterRoutes(e *echo.Echo) {
    // Map the GET request at path "/healthz" to the Health handler.  This
    // endpoint can be used by load balancers or monitoring systems to verify
    // that the service is up and running.
    e.GET("/healthz", handler.Health)
}

// RegisterAuth registers all authentication-related routes and their middleware.
// The provided AuthHandler implements the logic for each endpoint, and the
// jwtSecret is used to sign and verify JWT tokens for protected routes.
// RegisterAuth registers all authentication‑related routes and applies the
// necessary middleware.  Unauthenticated operations live under /v1/auth,
// while protected endpoints live under /v1.
func RegisterAuth(e *echo.Echo, a *handler.AuthHandler, jwtSecret string) {
    // Create a route group under the /v1/auth prefix for operations that do
    // not require an existing session (register, login, refresh).  Each of
    // these handlers is responsible for generating or exchanging tokens.
    g := e.Group("/v1/auth")
    // Register a POST endpoint to handle user registration at /v1/auth/register.
    g.POST("/register", a.Register)
    // Register a POST endpoint to handle user login at /v1/auth/login.
    g.POST("/login", a.Login)
    // Register a POST endpoint to refresh access tokens at /v1/auth/refresh.
    g.POST("/refresh", a.Refresh)
    // Register a POST endpoint to log out using a refresh token.  Unlike
    // previous iterations, logout does not require JWT authentication. The
    // handler accepts a JSON body containing a `refresh_token` and will
    // invalidate that token.  If the token is valid, a 204 response is
    // returned; otherwise 400/401/500 are possible depending on the error.
    g.POST("/logout", a.Logout)

    // Create another group for routes that require a valid access token.  All
    // handlers registered on this group will execute the JWTAuth middleware
    // before being invoked.  Protected endpoints live under /v1.
    auth := e.Group("/v1")
    // Apply the JWTAuth middleware to the protected group using the provided secret.
    auth.Use(middleware.JWTAuth(jwtSecret))
    // Register a GET endpoint at /v1/me that returns the authenticated user's information.
    auth.GET("/me", a.Me)

    // Additionally map POST /v1/logout to the same handler.  This route lives
    // at the top level (outside of the protected group) so it does not
    // require a JWT.  Clients can therefore call either /v1/auth/logout or
    // /v1/logout with a valid refresh token in the body to terminate a
    // session.
    e.POST("/v1/logout", a.Logout)
}
