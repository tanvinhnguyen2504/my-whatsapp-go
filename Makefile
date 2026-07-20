BINARY := bin/server
PKG    := ./cmd

.DEFAULT_GOAL := help

## help: show this list
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | awk -F': ' '{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

# ---- LOCAL ----

run:
	go run $(PKG)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BINARY) $(PKG)

tidy:
	go mod tidy

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test ./...

check: vet build
	@test -z "$$(gofmt -l .)" || (echo "unformatted files:"; gofmt -l .; exit 1)


up:
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs -f app

# ---- PRODUCTION ----

## docker-build: build the production image
docker-build:
	docker build -t my-whatsapp .

## docker-run: run the production image (http://localhost:8888)
docker-run:
	docker run --rm --env-file .env -p 8888:8888 -v whatsmeow-data:/app/data my-whatsapp

# ---- housekeeping ----

## qr: fetch the current login QR from a running server (PORT=8888 by default)
qr:
	@curl -s localhost:$${PORT:-8888}/qr

## db-reset: drop & recreate the Postgres schema (DB_SCHEMA from .env) so whatsmeow rebuilds its tables
db-reset:
	@eval "$$(grep -E '^DB_[A-Za-z_]+=' .env)"; \
	: "$${DB_SCHEMA:?DB_SCHEMA is not set in .env}"; \
	echo "Resetting schema \"$$DB_SCHEMA\" in \"$$DB_NAME\" on $$DB_HOST:$$DB_PORT ..."; \
	PGPASSWORD="$$DB_PASS" psql -h "$$DB_HOST" -p "$$DB_PORT" -U "$$DB_USER" -d "$$DB_NAME" -v ON_ERROR_STOP=1 \
		-c "DROP SCHEMA IF EXISTS \"$$DB_SCHEMA\" CASCADE;" \
		-c "CREATE SCHEMA \"$$DB_SCHEMA\" AUTHORIZATION \"$$DB_USER\";"
	@echo "Done. Start the server (make run) and whatsmeow will recreate its tables."

## clean: remove build artifacts and Air's tmp dir
clean:
	rm -rf bin tmp

.PHONY: help run build tidy fmt vet test check up down logs docker-build docker-run qr db-reset clean
