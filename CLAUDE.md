# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Architecture

The server drives **one of two selectable WhatsApp workflows**, chosen at startup by
`WHATSAPP_PROVIDER`, behind a single `whatsapp.Provider` interface so the REST layer is
identical for both:

- `business` — **WhatsMeow** (unofficial, personal account). Logs in via QR code, keeps its
  multi-device session in a **PostgreSQL** store (`pgx` driver, pure Go / no CGO), so the
  `whatsmeow_*` tables can be inspected directly in a local Postgres for debugging.
  Inbound messages arrive through an in-process event handler.
- `api` — **Meta WhatsApp Business Cloud API** (official). Stateless HTTPS calls to
  `graph.facebook.com`; inbound messages arrive via the `/webhook` HTTP callback (Meta's
  `GET` verification handshake echoes `hub.challenge` using `WHATSAPP_API_WEBHOOK_VERIFY_TOKEN`).

Provider selection lives in `internal/whatsapp/manager.go` (`New`). Adding media/document/
sticker sends means adding methods to `Provider` and implementing them in both files.

Layout: `cmd/` entrypoint → `internal/config` (env) → `internal/whatsapp` (providers) →
`internal/api` (Gin routes, handlers, webhook) → `internal/scheduler` (in-memory timed sends).

### Commands

```
go run ./cmd            # run the server
go build ./...          # build everything
go vet ./... && gofmt -l .   # vet + formatting check
go test ./...           # run all tests (none yet)

docker compose up       # dev with hot reload (Air rebuilds on .go changes)
docker build -t my-whatsapp . && docker run --env-file .env -p 8082:8082 my-whatsapp  # prod
```

### HTTP endpoints

`GET /health` · `GET /qr` (WhatsMeow login QR) · `POST /messages/text` ·
`POST /messages/schedule` · `GET|POST /webhook` (Business Cloud API).

## Environment Variables

Configuration is loaded from `.env` (via `internal/config`); see `.env.example` for the
full template. Loading fails fast if the selected provider is misconfigured.

- `MODE` — development | production
- `HTTP_PORT` — server port (default `8082`)
- `WHATSAPP_PROVIDER` — `api` (Meta Cloud API) or `business` (WhatsMeow)
- `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_PASS` / `DB_NAME` — PostgreSQL session store,
  **required by the `business` workflow** (`DB_HOST`, `DB_USER`, `DB_NAME` must be set)
- `WHATSAPP_BUSINESS_PHONE_NUMBER_ID` / `WHATSAPP_BUSINESS_ACCESS_TOKEN` /
  `WHATSAPP_BUSINESS_API_VERSION` — required by the `api` workflow
- `WHATSAPP_API_WEBHOOK_VERIFY_TOKEN` — echoes Meta's webhook `GET` verification handshake

## Project Goal

Build a Go server using WhatsMeow.

The server represents one WhatsApp account.

The server exposes REST APIs for sending WhatsApp messages.

The server is NOT intended to use the Meta Cloud API.

The project should focus on simplicity, readability, and rapid development.

---

## Tech Stack

- Go
- Gin
- WhatsMeow
- SQLite (development)
- PostgreSQL (optional)
- Docker

---

## Supported Message Types

- Text
- Image
- Video
- Audio
- Voice
- Document (PDF, DOCX, XLSX...)
- Sticker

---

## Main Features

- Login via QR Code
- Persist session
- Auto reconnect
- Send messages
- Receive messages
- Upload media
- Download media
- REST API
- Webhook/Event callbacks
- Scheduled messages

---

## Coding Rules

- Keep code simple.
- Prefer composition over abstraction.
- No unnecessary interfaces.
- Do not overengineer.
- One responsibility per package.
- Functions should be short.
- Return errors instead of panic.
- Use context.Context.
- Use slog for logging.

---

## Folder Structure

cmd/
internal/
    whatsapp/
    api/
    scheduler/
pkg/

---

## Goal

Whenever implementing a feature, prioritize working software over perfect architecture.

<!-- 
Build a lightweight WhatsApp Gateway in Go using WhatsMeow. The server acts as a programmable WhatsApp client for one personal account, exposing REST APIs to send and receive messages of various types (text, images, videos, audio, documents, stickers). The focus is simplicity and working software, not enterprise architecture. -->

## Comments
Only comments with the complex business logic
