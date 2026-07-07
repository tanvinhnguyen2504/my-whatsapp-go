# my-whatsapp

A small Go server that sends/receives WhatsApp messages over a REST API. It supports two
selectable workflows behind one interface:

| `WHATSAPP_PROVIDER` | Workflow | Login | Inbound |
|---|---|---|---|
| `api` | WhatsMeow (unofficial, personal account) | QR code | in-process event handler |
| `business` | Meta WhatsApp Business Cloud API (official) | access token | `/webhook` callback |

## Run locally

```bash
cp .env.example .env      # then edit as needed
go run ./cmd
```

For the `api` workflow, fetch the login QR after startup and scan it from
WhatsApp → Linked devices:

```bash
curl -s localhost:8082/qr
```

## Run with Docker

```bash
docker compose up         # dev: hot reload via Air on every .go change
# app is exposed on http://localhost:8082
```

Production image (static binary, no CGO):

```bash
docker build -t my-whatsapp .
docker run --env-file .env -p 8082:8082 -v whatsmeow-data:/app/data my-whatsapp
```

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| GET | `/health` | server + provider readiness |
| GET | `/qr` | current login QR (WhatsMeow) |
| POST | `/messages/text` | `{"to":"628123456789","body":"hi"}` |
| POST | `/messages/schedule` | as above plus `"at":"2026-07-07T15:04:05Z"` |
| GET/POST | `/webhook` | Meta Cloud API verification + inbound events |

## Layout

```
cmd/                    entrypoint + graceful shutdown
internal/config         env loading + validation
internal/whatsapp       Provider interface, WhatsMeow + Business implementations, manager
internal/api            Gin router, handlers, webhook
internal/scheduler      in-memory timed sends
```
