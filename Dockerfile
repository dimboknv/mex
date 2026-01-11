# Build stage
FROM golang:1.25-alpine AS builder

# Install git for fetching dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build both applications
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /tg_bot ./cmd/tg-bot/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /web_app ./cmd/web-app/

# Final stage for tg-bot
FROM alpine:3.19 AS tg-bot

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /tg_bot /app/tg_bot

# Create directory for data
RUN mkdir -p /app/data

EXPOSE 8080

ENTRYPOINT ["/app/tg_bot"]

# Final stage for web-app
FROM alpine:3.19 AS web-app

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder (web static files are embedded via go:embed)
COPY --from=builder /web_app /app/web_app

# Create directory for data
RUN mkdir -p /app/data

EXPOSE 8080

ENTRYPOINT ["/app/web_app"]
