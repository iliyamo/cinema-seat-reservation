<!--
This README documents the Cinema Seat Reservation System.  It follows
the style of the example provided in the user prompt: sections for
overview, architecture, domain flows, data model, project structure,
setup, API surface, concurrency, caching, security and improvements.
Citations reference external articles about row‑level locking,
token‑bucket rate limiting and RabbitMQ delivery semantics.
-->

# Cinema Seat Reservation System

## 📚 Overview

The **Cinema Seat Reservation System** is a backend service written in
Go for managing movie theatres and online ticket booking.  It offers
user registration and authentication, allows cinema owners to create
and manage cinemas, halls, seats and shows, and lets customers browse
shows, temporarily hold seats and confirm reservations.  Persistent
state is stored in **MySQL**, **Redis** provides caching, per‑user
rate‑limit buckets and temporary seat holds, and **RabbitMQ** handles
asynchronous event delivery for booking confirmations.  The HTTP
surface is served by the Echo web framework and follows REST
semantics.  Access tokens are issued as JWTs and refresh tokens are
hashed and persisted.

Unlike a naive CRUD application, this system emphasises scalability
and concurrency.  Critical sections such as placing a hold on a seat
are executed inside transactions and use `SELECT … FOR UPDATE` to
lock `show_seats` rows and prevent concurrent reservations from
reading or writing the same seat【75496455918405†L39-L44】.  A
token‑bucket algorithm implemented in Redis throttles how often a
customer can hold seats【262193549312775†L146-L154】.  Confirmed
reservations emit events to a durable RabbitMQ queue; acknowledgements
guarantee that messages are delivered at least once【357548365821013†L153-L155】.
A background consumer logs these events and can be extended to send
notifications or trigger analytics.

## 🏗 Architecture

### High‑level architecture

The system consists of a stateless API server, a persistence layer
(MySQL), a caching and queue layer (Redis + RabbitMQ) and a
background consumer.  The following diagram illustrates their
interactions:

```mermaid
graph TD
  user[Customer / Owner] -->|HTTP| api[API Server]
  api --> jwt[JWT Auth & Rate Limiter]
  jwt --> logic
  logic -->|Queries / transactions| mysql[(MySQL)]
  logic -->|Cache & holds / tokens| redis[(Redis)]
  logic -->|Publish booking events| mq[(RabbitMQ)]
  mq --> consumer[Booking Consumer]
  consumer --> logs[(booking.log)]
```

* **API Server**: Stateless Echo application exposing REST endpoints
  for authentication, public browsing, customer reservations and
  owner management.  Every request flows through JWT authentication
  and a token‑bucket rate limiter.
* **MySQL**: Stores persistent data (users, roles, cinemas, halls,
  seats, seat holds, shows, show seats, reservations,
  reservation seats and refresh tokens).  `SELECT … FOR UPDATE`
  protects rows during seat holds【75496455918405†L39-L44】.
* **Redis**: Caches responses to GET requests, holds per‑user
  rate‑limit buckets and temporary seat holds.  Its low latency
  improves performance.
* **RabbitMQ**: Durable AMQP broker for booking events.  Producers
  publish a `BookingConfirmedEvent` only after a successful
  transaction; consumers acknowledge messages after logging to ensure
  **at‑least‑once** delivery【357548365821013†L153-L155】.
* **Booking Consumer**: Background process consuming the
  `booking.confirmed` queue.  It appends human‑readable entries to
  `logs/booking.log` and can be extended to integrate notifications or
  analytics.

### Domain flows

#### Authentication

Users register with an email and password.  Passwords are salted and
hashed with bcrypt.  Successful login returns a short‑lived access
token (15 minutes) and a long‑lived refresh token (7 days) signed
with a shared secret.  Tokens embed the user’s role (`CUSTOMER` or
`OWNER`), and refresh tokens are stored in the database as SHA‑256
hashes for revocation.  The `/v1/auth/refresh` endpoint exchanges a
refresh token for a new access token; `/v1/logout` invalidates all of
a user’s refresh tokens.

#### Public browsing

Unauthenticated clients can discover the catalogue:

* **List cinemas** (`GET /v1/cinemas`)
* **List halls** of a cinema (`GET /v1/cinemas/{id}/halls`)
* **List shows** in a hall (`GET /v1/halls/{id}/shows`)
* **Show details** (`GET /v1/shows/{id}`)
* **Seat layout** (`GET /v1/halls/{id}/seats/layout`)
* **Seat availability** for a show (`GET /v1/shows/{id}/seats`) –
  returns status (`FREE`, `HELD`, `RESERVED`) and price per seat.
* **Flat seat list** (`GET /v1/halls/{id}/seats`) – optional `active` filter.
* **Search shows** (`GET /v1/search/shows`) – supports title
  searching and cursor‑based pagination.

Results are cached in Redis for a short TTL (default 30 seconds) and
invalidated after writes.

#### Seat holds and reservations

Customers (authenticated with role `CUSTOMER`) can reserve seats in a
two‑step process:

1. **Hold seats** (`POST /v1/shows/{id}/hold`): Receive a list of
   seat IDs, rate‑limit the request using a token‑bucket algorithm in
   Redis【262193549312775†L146-L154】, lock each seat using
   `SELECT … FOR UPDATE`【75496455918405†L39-L44】, and insert a row into
   `seat_holds`.  Holds expire automatically after a configured
   duration.
2. **Confirm seats** (`POST /v1/shows/{id}/confirm`): Verify that
   the seat holds exist and are still valid, calculate the total
   price, insert a row into `reservations` and `reservation_seats`,
   update `show_seats.status` to `RESERVED` and emit a
   `BookingConfirmedEvent` to RabbitMQ.  The booking consumer logs the
   event and other services can react asynchronously.

Customers can release their holds (`DELETE /v1/shows/{id}/hold`),
list reservations (`GET /v1/my-reservations`), view details of a
specific reservation (`GET /v1/reservations/{id}`) and cancel a
reservation (`DELETE /v1/reservations/{id}`) before the show starts.

#### Owner operations

Owners (authenticated with role `OWNER`) manage resources:

* **Cinemas**: Create (`POST /v1/cinemas`), update (`PUT/PATCH`)
  and delete (`DELETE`) cinemas.
* **Halls**: Create, update and delete halls.  A hall may belong to
  a cinema and defines optional row/column counts for automatically
  generating seats.
* **Seats**: Create, update and delete seats; seats have row
  labels, numbers, types (STANDARD, VIP, ACCESSIBLE) and an
  `is_active` flag.
* **Shows**: Schedule screenings by creating shows with a title,
  start/end times, base price and status.  Update or delete shows.
* **Reservations**: List reservations for a show, view details of a
  reservation and cancel a reservation.  Owner‑specific endpoints
  reside under `/v1/owner/reservations`.

## 🗃 Data model

Database migrations live under `internal/Docs`.  The core tables are:

| Table               | Purpose                                                     |
|---------------------|-------------------------------------------------------------|
| **roles**           | Enumerates allowed roles (`CUSTOMER`, `OWNER`).            |
| **users**           | Accounts with email, password hash, role/role_id and flags. |
| **refresh_tokens**  | Hashed refresh tokens with user ID, expiry and revocation. |
| **cinemas**         | Cinemas owned by users; name and timestamps.               |
| **halls**           | Screening halls; optional cinema_id, name, description and seat grid dimensions. |
| **seats**           | Physical seats in a hall; row label, seat number, type and active flag. |
| **seat_holds**      | Temporary holds during checkout; expire after a timeout.   |
| **shows**           | Scheduled screenings; title, hall_id, start/end, base price and status. |
| **show_seats**      | One row per seat per show; tracks status (`FREE`, `HELD`, `RESERVED`), price and version for optimistic locking. |
| **reservations**    | User bookings; show_id, status (`PENDING`, `CONFIRMED`, `CANCELLED`), total amount and optional payment reference. |
| **reservation_seats** | Links reservations to individual seats with their price.   |

Foreign keys maintain referential integrity (e.g.
`show_seats.show_id → shows.id` and `reservation_seats.seat_id → seats.id`).
Indexes on frequently queried columns (email, role_id, hall_id, show_id,
seat_id) improve performance.  The `version` column in `show_seats`
enables optimistic locking when updating seat status.

## 📂 Project structure

The repository follows a clean architecture that separates concerns:

```
cinema-seat-reservation/
├── cmd/
│   └── server/            # entry point with main.go
├── internal/
│   ├── Docs/              # SQL migrations and (optionally) diagrams
│   ├── config/            # configuration loaders (Redis, rate limiting, caching)
│   ├── database/          # DB initialisation and connection helpers
│   ├── handler/           # HTTP handlers (auth, customer, owner, public)
│   ├── middleware/        # JWT auth, rate limiting, caching, role checks
│   ├── model/             # Domain structs mapping to database tables
│   ├── queue/             # RabbitMQ event definitions and consumer
│   ├── repository/        # Data access layer with transactions and locking
│   ├── router/            # Route definitions grouped by role and area
│   ├── service/           # Auxiliary services (e.g. queue publisher)
│   └── utils/             # Helpers (JWT generation, password hashing)
├── docker-compose.yml     # Dev environment (app + MySQL + Redis + RabbitMQ)
├── Dockerfile             # Build instructions for the API server
├── go.mod, go.sum         # Go module files
└── README.md              # This document
```

## ⚙️ Setup and running

### Prerequisites

You can run the service via Docker Compose or manually.  Manual
execution requires:

* **Go 1.21+** – for compiling and running the application.
* **MySQL 8+** – create a database and apply migrations from
  `internal/Docs`.
* **Redis 6+** – caching, rate limiting and seat holds.
* **RabbitMQ 3.8+** – message broker for booking events.

### Environment variables

Copy `.env.example` to `.env` and adjust values.  Important variables:

| Variable                     | Description                                            | Example |
|------------------------------|--------------------------------------------------------|---------|
| `APP_PORT`                  | Port for the HTTP server                              | `8080` |
| `APP_ENV`                   | Environment (e.g. `dev`, `prod`)                      | `dev` |
| `DB_USER` / `DB_PASS`       | MySQL credentials                                     | `your_db_user` / `your_db_password` |
| `DB_HOST` / `DB_PORT`       | MySQL host and port                                   | `127.0.0.1` / `3306` |
| `DB_NAME`                   | Database name                                         | `cinema` |
| `JWT_SECRET`                | Secret used to sign JWTs                              | long random string |
| `ACCESS_TOKEN_TTL_MIN`      | Access token lifetime in minutes                      | `15` |
| `REFRESH_TOKEN_TTL_DAYS`    | Refresh token lifetime in days                        | `7` |
| `BCRYPT_COST`               | Cost factor for password hashing                      | `12` |
| `REDIS_HOST` / `REDIS_PORT` | Redis host and port                                   | `127.0.0.1` / `6379` |
| `REDIS_DB`                  | Redis database index                                  | `0` |
| `REDIS_PASSWORD`            | Redis password (if any)                               | (empty) |
| `REDIS_TLS`                 | Use TLS when connecting to Redis                      | `false` |
| `RATE_LIMIT_BUCKET_CAPACITY`| Maximum tokens in rate‑limit bucket                   | `60` |
| `RATE_LIMIT_REFILL_TOKENS`  | Tokens added per refill interval                      | `3` |
| `RATE_LIMIT_REFILL_INTERVAL`| Interval at which tokens are refilled                 | `5s` |
| `RATE_LIMIT_KEY_STRATEGY`   | Key strategy for rate limiting (e.g. `ip_user`)       | `ip_user` |
| `RATE_LIMIT_ENTRY_TTL`      | TTL for rate‑limit entries                            | `15m` |
| `RABBITMQ_URL`              | AMQP URI for RabbitMQ                                 | `amqp://guest:guest@127.0.0.1:5672/` |
| `CACHE_ENABLED`             | Enable response caching                               | `true` |
| `CACHE_TTL`                 | Default TTL for cached responses                      | `30s` |
| `CACHE_KEY_STRATEGY`        | Strategy for cache keys (`route_query`, etc.)         | `route_query` |
| `CACHE_PREFIX`              | Prefix for cache keys                                 | `cache` |
| `CACHE_MAX_BODY_BYTES`      | Maximum size of response bodies to cache (bytes)      | `1048576` |

### Running with Docker Compose

To spin up all dependencies at once:

```bash
git clone <your-fork>
cd cinema-seat-reservation
cp .env.example .env   # configure variables
docker-compose up --build
```

This will start the API server, a MySQL instance seeded via the
migrations, a Redis server and a RabbitMQ broker.  The API listens
on `http://localhost:8080/v1` and RabbitMQ’s management UI is
available at `http://localhost:15672` (user `guest`/`guest`).

### Running without Docker

If you prefer to run services manually, ensure MySQL, Redis and
RabbitMQ are available and set the environment variables above.
Then build and run the server:

```bash
go mod download
go run ./cmd/server
```

Apply the SQL migrations under `internal/Docs` to initialise the
database before starting the server.

## 🌐 API surface

All endpoints live under `/v1`.  Endpoints marked **(Auth)** require
a valid access token (`Authorization: Bearer <token>`) and respect
rate limiting.  A concise overview:

### Authentication

| Method & path             | Description                                                      | Notes      |
|---------------------------|------------------------------------------------------------------|------------|
| `POST /v1/auth/register`  | Register a new user with email and password                      | Returns 201 |
| `POST /v1/auth/login`     | Log in and obtain access & refresh tokens                        | Returns 200 |
| `POST /v1/auth/refresh`   | Exchange a refresh token for a new access token                  | Validates stored token |
| `POST /v1/auth/refresh-access` | Refresh access token without rotating the refresh token         |             |
| `POST /v1/auth/logout`    | Invalidate a refresh token                                       |             |
| `GET  /v1/me`             | Retrieve the authenticated user’s details                       | **(Auth)** |

### Public

| Method & path                                 | Description                                             | Notes |
|-----------------------------------------------|---------------------------------------------------------|-------|
| `GET /v1/cinemas`                             | List all cinemas                                        |       |
| `GET /v1/cinemas/{id}/halls`                  | List halls in a cinema                                  |       |
| `GET /v1/halls/{id}/shows`                    | List shows in a hall                                    |       |
| `GET /v1/shows/{id}`                          | Get show details                                        |       |
| `GET /v1/halls/{id}/seats/layout`             | Get seat layout (rows & columns) for a hall             |       |
| `GET /v1/shows/{id}/seats`                    | Get seat availability for a show                        |       |
| `GET /v1/halls/{id}/seats`                    | List seats in a hall (flat list; filterable by `active`) |       |
| `GET /v1/search/shows`                        | Search shows by title with cursor‑based pagination      |       |

### Customers

| Method & path                          | Description                                                             | Notes            |
|----------------------------------------|-------------------------------------------------------------------------|------------------|
| `POST /v1/shows/{id}/hold`             | Hold selected seats                                                     | **(Auth)**       |
| `DELETE /v1/shows/{id}/hold`           | Release held seats                                                      | **(Auth)**       |
| `POST /v1/shows/{id}/confirm`          | Confirm held seats and create a reservation                            | **(Auth)**       |
| `GET /v1/my-reservations`              | List reservations for the authenticated user                           | **(Auth)**       |
| `GET /v1/reservations/{id}`            | Get details of a specific reservation                                  | **(Auth)**       |
| `DELETE /v1/reservations/{id}`         | Cancel a reservation before the show starts                             | **(Auth)**       |

### Owners

| Method & path                               | Description                                                           | Notes      |
|---------------------------------------------|-----------------------------------------------------------------------|------------|
| `POST /v1/cinemas`                          | Create a cinema                                                      | **(Auth)** |
| `PUT/PATCH /v1/cinemas/{id}`                | Update a cinema                                                      | **(Auth)** |
| `DELETE /v1/cinemas/{id}`                   | Delete a cinema                                                      | **(Auth)** |
| `POST /v1/halls`                            | Create a hall                                                        | **(Auth)** |
| `PUT/PATCH /v1/halls/{id}`                  | Update a hall                                                        | **(Auth)** |
| `DELETE /v1/halls/{id}`                     | Delete a hall                                                        | **(Auth)** |
| `POST /v1/seats`                            | Create a seat                                                        | **(Auth)** |
| `PUT/PATCH /v1/seats/{id}`                  | Update a seat                                                        | **(Auth)** |
| `DELETE /v1/seats/{id}`                     | Delete a seat                                                        | **(Auth)** |
| `POST /v1/shows`                            | Create a show                                                        | **(Auth)** |
| `PUT/PATCH /v1/shows/{id}`                  | Update a show                                                        | **(Auth)** |
| `DELETE /v1/shows/{id}`                     | Delete a show                                                        | **(Auth)** |
| `GET /v1/shows/{id}/reservations`           | List reservations for a show                                         | **(Auth)** |
| `GET /v1/owner/reservations/{id}`           | Get a reservation’s details from the owner’s perspective              | **(Auth)** |
| `DELETE /v1/owner/reservations/{id}`        | Cancel a reservation (owner override)                                 | **(Auth)** |

## 🧠 Concurrency and race conditions

### Seat holds

When multiple customers try to hold the same seats concurrently, the
application must ensure that only one hold succeeds.  The
`HoldSeats` handler runs inside a database transaction and executes
`SELECT … FOR UPDATE` on each `show_seat` row【75496455918405†L39-L44】.
This locks the row until the transaction commits or rolls back,
preventing other transactions from reading or updating the seat
status.  Seat holds are stored in `seat_holds` with an `expires_at`
timestamp; expired holds are cleaned up before new holds are placed.

### Rate limiting

To protect the system from abuse, hold operations are rate‑limited
using a token‑bucket algorithm in Redis【262193549312775†L146-L154】.  Each
user has a bucket with a capacity (`RATE_LIMIT_BUCKET_CAPACITY`) and a
refill rate (`RATE_LIMIT_REFILL_TOKENS` / `RATE_LIMIT_REFILL_INTERVAL`).
Each hold request consumes a token; if no tokens remain, the request
is rejected with **429 Too Many Requests**.  Although the current
implementation reads, modifies and writes the bucket state without
atomicity, it can be improved by using Lua scripts or Redis
transactions to perform the refill, check and decrement atomically.

### Messaging semantics

Events are published to RabbitMQ with persistent delivery.  Consumers
acknowledge messages only after processing them.  This yields
**at‑least‑once** delivery semantics【357548365821013†L153-L155】—messages
may be delivered more than once but will not be lost.  When
RabbitMQ is unavailable at startup, the system falls back to Redis
Pub/Sub, which provides **at‑most‑once** delivery; messages sent when
no consumer is subscribed may be lost.

## 📦 Caching, search and Redis

Redis is used both as a cache and as a fallback broker:

* **Response caching**: Frequently accessed endpoints (cinema lists,
  halls, shows, seat layouts, seat availability and search results)
  are cached under keys derived from the route and query
  parameters.  Entries expire after a TTL (default 30 seconds) and
  are invalidated on writes to maintain consistency.
* **Rate limiting**: Each user’s rate‑limit bucket is stored as a
  JSON object keyed by `rate:<user_id>` with a TTL longer than the
  time needed to refill the bucket.
* **Seat holds**: Temporary seat holds are stored both in the
  database (`seat_holds`) and optionally in Redis for quick lookup;
  holds expire automatically.
* **Fallback broker**: If RabbitMQ cannot be reached, Redis Pub/Sub
  is used to publish and consume booking events.  Because Pub/Sub
  messages are not persisted, this fallback offers only at‑most‑once
  delivery.

## 🔐 Security considerations

Security measures in the system include:

* **Password hashing**: Passwords are salted and hashed using bcrypt
  before being stored.  Plaintext passwords are never persisted.
* **JWT tokens**: Access tokens expire after the configured
  `ACCESS_TOKEN_TTL_MIN` and embed user ID and role.  Refresh tokens
  expire after `REFRESH_TOKEN_TTL_DAYS` and are stored in the
  `refresh_tokens` table hashed with SHA‑256.  Only valid,
  unexpired refresh tokens can be exchanged.
* **Role enforcement**: Middleware ensures that only users with the
  appropriate role can access customer or owner routes.
* **Environment secrets**: Secrets such as database passwords and
  JWT signing keys are provided via environment variables and should
  never be committed to version control.  Use a secrets manager in
  production.
* **Transport security**: Deploy behind an HTTPS reverse proxy
  (nginx, Caddy, etc.) and enable TLS on database and broker
  connections.

## 🧭 Notes & improvements

This project serves as a learning exercise for structuring a Go
backend with messaging and caching.  Potential improvements include:

* **Atomic rate limiting**: Replace the read–modify–write cycle with
  a Lua script or Redis transactions to avoid races.
* **Distributed locking**: For horizontally scaled deployments,
  implement a distributed mutex (e.g. Redlock) or database advisory
  locks to guarantee exclusive updates to shared resources (e.g.
  seat status).
* **Transactional outbox**: Persist pending booking events in a
  database outbox within the same transaction as reservation updates
  and have a separate process publish them to RabbitMQ to achieve
  exactly‑once semantics.
* **Monitoring and tracing**: Integrate Prometheus metrics and
  OpenTelemetry tracing to monitor request rates, queue lengths and
  latencies.
* **Testing**: Add unit tests for handlers and repositories and use
  integration tests with testcontainers for MySQL, Redis and
  RabbitMQ.
* **Role‑based authorisation**: Extend roles beyond customer and
  owner (e.g. admin) and enforce more granular permissions.
* **Front‑end**: Develop a web or mobile interface that consumes
  this API.

Contributions are welcome!  Please open an issue or submit a pull
request if you discover bugs or want to propose enhancements.