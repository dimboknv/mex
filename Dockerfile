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
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /tg_bot ./cmd/tg-bot/
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /web_app ./cmd/web-app/

# Final stage with both binaries
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy both binaries from builder
COPY --from=builder /tg_bot /app/tg_bot
COPY --from=builder /web_app /app/web_app

# Create directory for data
RUN mkdir -p /app/data

EXPOSE 8080
