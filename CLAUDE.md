# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

MEXC Copy Trading monorepo with two independent Go applications for automated futures trading on the MEXC exchange:
- **Telegram Bot** (`cmd/tg-bot/`) - Copy trading controlled via Telegram commands
- **Web App** (`cmd/web-app/`) - REST API with JWT auth and web frontend

Both apps share the same core services in `pkg/` for MEXC API interaction, WebSocket connections, and copy trading logic.

## Build Commands

```bash
# Build Telegram Bot
cd cmd/tg-bot && go build -o tg_bot

# Build Web App
cd cmd/web-app && go build -o web_app

# Run from project root
./cmd/tg-bot/tg_bot
./cmd/web-app/web_app

# Download dependencies
go mod download

# Fix module issues (shown as diagnostics)
go mod tidy
```

## Environment Variables

**Telegram Bot:**
- `TELEGRAM_BOT_TOKEN` - Required
- `DRY_RUN` - `true` (default) for simulation, `false` for real trades

**Web App:**
- `ADDRESS` - Listen address (default: `:8080`)
- `JWT_SECRET` - JWT signing key (required in production)
- `DB_PATH` - SQLite database path (default: `./web_app.db`)
- `API_URL` - Base URL for frontend and mirror script (default: `http://localhost:8080`)
- `DRY_RUN` - `true` (default) for simulation, `false` for real trades

## Architecture

### Package Structure

- `cmd/` - Application entry points with multi-handler logging (stdout + file)
- `pkg/` - Shared packages used by both applications:
  - `pkg/services/copytrading/` - Core copy trading logic with WebSocket event handlers
  - `pkg/services/mexc/` - MEXC REST API client (orders, positions, leverage, stop-loss)
  - `pkg/services/websocket/` - WebSocket client for real-time order events from master account
  - `pkg/storage/` - SQLite storage for accounts (Telegram bot uses `Storage`, Web uses `WebStorage`)
  - `pkg/models/` - Shared data models
- `internal/` - Web-app specific code:
  - `internal/api/` - REST API handlers with gorilla/mux router
  - `internal/auth/` - JWT authentication service
  - `internal/middleware/` - CORS and auth middleware
  - `internal/handlers/` - Telegram bot command handlers
- `web/` - Static frontend files for Web App
- `migrations/` - SQL schema migrations

### Copy Trading Flow

1. Master account connects via WebSocket (`pkg/services/websocket/`)
2. WebSocket client receives order/position events from MEXC
3. Copy trading service (`pkg/services/copytrading/service.go`) processes events
4. For each event, parallel goroutines execute corresponding actions on slave accounts via MEXC REST API
5. Events: `OrderEvent`, `StopOrderEvent`, `StopPlanOrderEvent`, `PositionEvent`, `DealEvent`

### Storage

Two separate storage implementations using `modernc.org/sqlite`:
- `pkg/storage/storage.go` - For Telegram bot (accounts keyed by `chat_id`)
- `pkg/storage/web-storage.go` - For Web app (accounts keyed by `user_id`, includes users table)

### Authentication

The Web app uses browser cookies extracted from MEXC via a JavaScript script. Accounts store:
- `uc_token` - Authentication token
- `u_id` - User ID
- `deviceId` - Device fingerprint
- Full cookie jar for API requests

## Key Patterns

- DRY_RUN mode: All trading actions are logged but not executed when enabled
- Multi-handler slog: Both apps log to stdout (pretty/colored) and file simultaneously
- Concurrent slave processing: Copy trading uses goroutines with `sync.WaitGroup` for parallel execution
- Graceful shutdown: Web app handles SIGINT/SIGTERM for clean shutdown