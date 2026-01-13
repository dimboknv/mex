# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

MEXC Copy Trading monorepo with two independent Go applications for automated futures trading on the MEXC exchange:
- **Telegram Bot** (`cmd/tg-bot/`) - Copy trading controlled via Telegram commands
- **Web App** (`cmd/web-app/`) - REST API with JWT auth and embedded web frontend

Both apps share the same SQLite database (`DB_PATH`) and core services in `internal/` for MEXC API interaction, WebSocket connections, and copy trading logic.

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

# Fix module issues
go mod tidy
```

## Environment Variables

**Telegram Bot:**
- `TELEGRAM_BOT_TOKEN` - Required
- `DB_PATH` - SQLite database path (default: same as web app for shared data)
- `DRY_RUN` - `true` (default) for simulation, `false` for real trades
- `WEBHOOK_URL` / `WEBHOOK_PATH` - Optional, for webhook mode (production)

**Web App:**
- `ADDRESS` - Listen address (default: `:8080`)
- `JWT_SECRET` - JWT signing key (required in production)
- `DB_PATH` - SQLite database path (default: `./web_app.db`)
- `API_URL` - Base URL for frontend and mirror script (default: `http://localhost:8080`)
- `DRY_RUN` - `true` (default) for simulation, `false` for real trades

## Architecture

### Package Structure

```
cmd/
├── tg-bot/main.go      # Telegram bot entry point
└── web-app/main.go     # Web app entry point

internal/
├── api/                # Web app REST API
│   ├── auth/           # JWT authentication service
│   ├── copytrading/    # Copy trading service layer & interfaces
│   ├── middleware/     # CORS and auth middleware
│   ├── web/            # Embedded static frontend (go:embed)
│   ├── handler.go      # Main API handler struct
│   └── router.go       # Route configuration
├── config/             # Environment variable loading
├── mexc/               # MEXC exchange integration
│   ├── client.go       # REST API client (orders, positions, leverage)
│   ├── copytrading/    # Copy trading engine & session management
│   │   ├── engine.go   # Trade execution engine
│   │   ├── service.go  # Manager & Session types
│   │   └── websocket/  # WebSocket-based copy trading
│   └── websocket/      # WebSocket client for MEXC events
├── models/             # Shared data models
├── storage/            # Unified SQLite storage (WebStorage used by both apps)
└── telegram/           # Telegram bot service & command handlers
    └── copytrading/    # Telegram-specific copy trading adapter
```

### Copy Trading Flow

1. Master account connects via WebSocket (`internal/mexc/websocket/`)
2. WebSocket receives order events from MEXC (`wss://contract.mexc.com/edge`)
3. Copy trading engine (`internal/mexc/copytrading/engine.go`) processes events
4. Parallel goroutines execute actions on slave accounts via MEXC REST API
5. Events: `OrderEvent`, `StopOrderEvent`, `StopPlanOrderEvent`, `PositionEvent`, `DealEvent`

### Copy Trading Modes (Web App)

1. **WebSocket Mode** - Direct WebSocket connection from master account for real-time event handling
2. **Mirror Mode** - JavaScript intercepts MEXC API calls in browser, forwards to backend via token auth

### Storage

Unified storage using `modernc.org/sqlite` in `internal/storage/web-storage.go`:
- Both apps use `WebStorage` for shared database access
- Users table with `telegram_chat_id` column for Telegram-to-user mapping
- Accounts keyed by `user_id` (FK to users table)
- Includes: trades history, trade_details, activity_log, copy_trading_sessions

### MEXC Account Authentication

Accounts use browser cookies extracted via JavaScript script:
- `uc_token` - Authentication token
- `u_id` - User ID
- `deviceId` - Device fingerprint
- Full cookie jar for API requests

## Key Patterns

- **Unified database**: Both Telegram bot and Web app share the same SQLite database
- **DRY_RUN mode**: Default enabled - all trading actions logged but not executed
- **Multi-handler slog**: Both apps log to stdout (colored via tint) and file simultaneously
- **Concurrent slave processing**: Uses `sync.WaitGroup` for parallel trade execution across accounts
- **Graceful shutdown**: Signal handlers for SIGINT/SIGTERM with clean resource cleanup
- **Interface-based storage**: `AccountStorage`, `TradeStorage`, `LogStorage`, `UserStorage` interfaces
- **Telegram user mapping**: Auto-creates user record when Telegram user first interacts (chatID → userID)