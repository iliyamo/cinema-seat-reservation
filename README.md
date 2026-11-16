# Cinema Seat Reservation API

A REST API for managing cinemas, halls, seats, screenings and ticket reservations. Built with Go, Echo and MySQL.

## Features

- User registration and login with `CUSTOMER` and `OWNER` roles.
- JWT access tokens and refresh-token management.
- Bcrypt password hashing.
- Public endpoints for browsing cinemas, halls, screenings and seat availability.
- Owner endpoints for managing cinemas, halls, seats and screenings.
- Customer workflows for temporarily holding seats, confirming bookings, viewing reservations and cancelling eligible reservations.
- Transactional reservation operations with MySQL row-level locks to protect seat availability.
- Five-minute seat holds; expired holds are cleaned up during the confirmation workflow.

## Tech stack

- **Language:** Go 1.24.3
- **HTTP framework:** Echo v4
- **Database:** MySQL
- **Authentication:** JWT
- **Password hashing:** bcrypt

The current codebase uses MySQL as its persistence layer. Redis, RabbitMQ, response caching and background message consumers are not part of the current implementation.

## Project structure

```text
.
├── cmd/server/          # Application entry point
├── internal/
│   ├── config/          # Environment-based configuration
│   ├── database/        # MySQL connection setup
│   ├── Docs/            # SQL schema and migrations
│   ├── handler/         # HTTP handlers
│   ├── middleware/      # JWT and role middleware
│   ├── model/           # Application models
│   ├── repository/      # MySQL data access
│   ├── router/          # API route registration
│   └── utils/           # JWT and password helpers
├── .env.example
├── go.mod
└── README.md
```

## Requirements

- Go 1.24.3 or newer
- MySQL
- A database created for the application

## Configuration

The application reads its configuration from environment variables and attempts to load a `.env` file at startup.

Set the following variables (the names below match the current configuration loader):

| Variable | Description |
| --- | --- |
| `APP_ENV` | Environment name, such as `dev` or `prod` |
| `APP_PORT` | HTTP server port |
| `DB_USER` | MySQL username |
| `DB_PASS` | MySQL password; may be empty |
| `DB_HOST` | MySQL hostname |
| `DB_PORT` | MySQL port |
| `DB_NAME` | Database name |
| `JWT_SECRET` | Secret used to sign JWTs |
| `ACCESS_TOKEN_TTL_MIN` | Access-token lifetime in minutes |
| `REFRESH_TOKEN_TTL_DAYS` | Refresh-token lifetime in days |
| `BCRYPT_COST` | bcrypt cost factor |

> **Note:** The current `.env.example` uses older names for the port and token-lifetime settings. Use the names in this table with the current application code.

## Database setup

Create the database, then apply the SQL migrations in `internal/Docs` in numerical order. The migration files define the users, refresh tokens, cinemas, halls, seats, screenings, seat holds and reservation tables.

## Run locally

1. Install Go and MySQL.
2. Create the database and apply the migrations.
3. Configure the required environment variables.
4. Start the API:

```bash
go mod download
go run ./cmd/server
```

The server listens on the port configured by `APP_PORT`. The health endpoint is available at:

```text
GET /healthz
```

## API overview

All application routes use the `/v1` prefix.

### Authentication

| Method | Endpoint | Description |
| --- | --- | --- |
| POST | `/v1/auth/register` | Create an account |
| POST | `/v1/auth/login` | Sign in |
| POST | `/v1/auth/refresh` | Rotate a refresh token and issue new tokens |
| POST | `/v1/auth/refresh-access` | Issue a new access token without rotating the refresh token |
| POST | `/v1/auth/logout` | Revoke a refresh token |
| GET | `/v1/me` | Get the authenticated user's ID and role |

### Public browsing

| Method | Endpoint | Description |
| --- | --- | --- |
| GET | `/v1/cinemas` | List cinemas |
| GET | `/v1/cinemas/:id/halls` | List halls in a cinema |
| GET | `/v1/halls/:id/shows` | List screenings in a hall |
| GET | `/v1/shows/:id` | Get screening details |
| GET | `/v1/halls/:id/seats/layout` | Get a hall's seat layout |
| GET | `/v1/halls/:id/seats` | List seats in a hall |
| GET | `/v1/shows/:id/seats` | Get seat availability for a screening |

### Customer

These endpoints require a valid JWT and the `CUSTOMER` role.

| Method | Endpoint | Description |
| --- | --- | --- |
| POST | `/v1/shows/:id/hold` | Hold available seats |
| DELETE | `/v1/shows/:id/hold` | Release the customer's active holds |
| POST | `/v1/shows/:id/confirm` | Confirm held seats and create a reservation |
| GET | `/v1/my-reservations` | List the customer's reservations |
| GET | `/v1/reservations/:id` | Get a reservation |
| DELETE | `/v1/reservations/:id` | Cancel a reservation before the screening starts |

### Owner

These endpoints require a valid JWT and the `OWNER` role.

| Method | Endpoint | Description |
| --- | --- | --- |
| POST | `/v1/cinemas` | Create a cinema |
| PUT, PATCH | `/v1/cinemas/:id` | Update a cinema |
| DELETE | `/v1/cinemas/:id` | Delete a cinema |
| POST | `/v1/halls` | Create a hall |
| PUT, PATCH | `/v1/halls/:id` | Update a hall |
| DELETE | `/v1/halls/:id` | Delete a hall |
| POST | `/v1/seats` | Create a seat |
| PUT, PATCH | `/v1/seats/:id` | Update a seat |
| DELETE | `/v1/seats/:id` | Delete a seat |
| POST | `/v1/shows` | Create a screening |
| PUT, PATCH | `/v1/shows/:id` | Update a screening |
| DELETE | `/v1/shows/:id` | Delete a screening |
| GET | `/v1/shows/:id/reservations` | List reservations for a screening |
| GET | `/v1/owner/reservations/:id` | Get a reservation |
| DELETE | `/v1/owner/reservations/:id` | Cancel a reservation |

## Seat reservation flow

1. A customer requests a hold for one or more seats.
2. The API checks availability inside a MySQL transaction and locks each relevant `show_seats` row with `SELECT ... FOR UPDATE`.
3. Available seats are recorded in `seat_holds` with a five-minute expiration and their status is changed to `HELD`.
4. The customer confirms the hold. The API validates ownership and expiration, creates the reservation and reservation-seat records, then marks the seats as `RESERVED` in the same transaction.
5. Customers can release holds or cancel a reservation before its screening starts.

## Security

- Passwords are stored as bcrypt hashes.
- Access tokens are JWTs.
- Refresh tokens are stored as SHA-256 hashes and can be revoked.
- Owner and customer routes are protected by JWT authentication and role checks.
- Database operations use parameterized SQL queries.

## Current scope

This repository contains the Go API and MySQL schema/migrations. A frontend, automated test suite, Docker-based deployment configuration and external cache or message-broker integrations are not currently included.
