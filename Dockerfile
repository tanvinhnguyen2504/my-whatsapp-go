# Production image: multi-stage, static binary (CGO disabled — pgx driver is pure Go).
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/server /app/server
RUN chown -R app:app /app
USER app

# Session state lives in PostgreSQL; supply DB_HOST/DB_USER/DB_NAME (and DB_PASS) at runtime.
ENV HTTP_PORT=8082
EXPOSE 8082
ENTRYPOINT ["/app/server"]
