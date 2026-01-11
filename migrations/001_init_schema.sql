-- Web App Database Schema

-- Пользователи веб-приложения
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Аккаунты MEXC
CREATE TABLE IF NOT EXISTS accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    token TEXT NOT NULL,
    user_id_mexc TEXT NOT NULL,
    device_id TEXT NOT NULL,
    cookies TEXT,
    user_agent TEXT,
    proxy TEXT,
    is_master INTEGER DEFAULT 0,
    disabled INTEGER DEFAULT 0,
    auto_disable_on_fee INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, name),
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_accounts_user ON accounts(user_id);
CREATE INDEX IF NOT EXISTS idx_accounts_master ON accounts(user_id, is_master);

-- История сделок (детальная)
CREATE TABLE IF NOT EXISTS trades (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    master_account_id INTEGER,
    symbol TEXT NOT NULL,
    side INTEGER NOT NULL,
    volume INTEGER NOT NULL,
    leverage INTEGER NOT NULL,
    sent_at DATETIME NOT NULL,
    received_at DATETIME,
    exchange_accepted_at DATETIME,
    status TEXT NOT NULL,
    error TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY(master_account_id) REFERENCES accounts(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_trades_user ON trades(user_id);
CREATE INDEX IF NOT EXISTS idx_trades_sent ON trades(sent_at DESC);
CREATE INDEX IF NOT EXISTS idx_trades_status ON trades(status);

-- Детали по каждому slave аккаунту в сделке
CREATE TABLE IF NOT EXISTS trade_details (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trade_id INTEGER NOT NULL,
    account_id INTEGER NOT NULL,
    status TEXT NOT NULL,
    error TEXT,
    order_id TEXT,
    latency_ms INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(trade_id) REFERENCES trades(id) ON DELETE CASCADE,
    FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_trade_details_trade ON trade_details(trade_id);
CREATE INDEX IF NOT EXISTS idx_trade_details_account ON trade_details(account_id);

-- Лог активности
CREATE TABLE IF NOT EXISTS activity_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER,
    level TEXT NOT NULL,
    action TEXT NOT NULL,
    message TEXT NOT NULL,
    details TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_activity_log_user ON activity_log(user_id);
CREATE INDEX IF NOT EXISTS idx_activity_log_created ON activity_log(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_activity_log_level ON activity_log(level);

-- Copy Trading Sessions
CREATE TABLE IF NOT EXISTS copy_trading_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    master_account_id INTEGER NOT NULL,
    is_active INTEGER DEFAULT 1,
    ignore_fees INTEGER DEFAULT 0,
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    stopped_at DATETIME,
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY(master_account_id) REFERENCES accounts(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_sessions_user ON copy_trading_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_active ON copy_trading_sessions(is_active);
