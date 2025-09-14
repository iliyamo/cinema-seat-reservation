## Makefile for cinema-seat-reservation API
#
# This Makefile provides convenient shortcuts for local development.
# Use `make help` to list available targets.  The default `make run`
# target runs the API locally using environment variables from a
# `.env` file if present.  The Docker related targets now use
# `docker compose` (with a space) to be compatible with both the
# standalone docker-compose binary and the built‑in plugin that ships
# with recent versions of Docker.

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  tidy   - tidy Go modules"
	@echo "  fmt    - format Go code"
	@echo "  build  - build binary (bin/app)"
	@echo "  run    - run locally using environment variables from .env"
	@echo "  test   - run unit tests"
	@echo "  up     - start the application stack with docker-compose"
	@echo "  down   - stop the application stack"
	@echo "  logs   - follow API logs from docker-compose"
	@echo "  seed   - manually seed the database (requires mysql client)"
	@echo "  lint   - run staticcheck (if installed)"

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: build
build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/app ./cmd/server

.PHONY: run
run:
	@if [ -f .env ]; then export $$(grep -v '^#' .env | xargs); fi; go run ./cmd/server

.PHONY: test
test:
	go test ./...

# Bring up the full stack (MySQL, Redis, RabbitMQ and the API) using
# docker compose.  Building the API image is performed automatically.
.PHONY: up
up:
	docker compose up -d --build

# Tear down the stack and remove containers
.PHONY: down
down:
	docker compose down

# Follow the API container logs.  Use `CTRL+C` to stop following.
.PHONY: logs
logs:
	docker compose logs -f api

# Optionally reseed the database manually. Requires mysql client installed
# Optionally reseed the database manually.  Requires the mysql client
# inside the mysql container and docker compose.  See internal/Docs
# for the seed file.
.PHONY: seed
seed:
    @echo "Seeding cinema.sql into mysql container..."
    docker cp ./internal/Docs/cinema.sql $$(docker compose ps -q mysql):/tmp/cinema.sql
    docker compose exec -T mysql sh -lc "mysql -u$$MYSQL_USER -p$$MYSQL_PASSWORD $$MYSQL_DATABASE < /tmp/cinema.sql"

.PHONY: lint
lint:
	@which staticcheck >/dev/null 2>&1 || (echo "install staticcheck: go install honnef.co/go/tools/cmd/staticcheck@latest" && exit 1)
	staticcheck ./...