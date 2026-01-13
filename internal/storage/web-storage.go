package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	models2 "tg_mexc/internal/models"

	_ "modernc.org/sqlite"
)

// WebStorage управляет базой данных веб-приложения
type WebStorage struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewWeb создает новый экземпляр WebStorage
func NewWeb(dbPath string, logger *slog.Logger) (*WebStorage, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	storage := &WebStorage{
		db:     db,
		logger: logger,
	}

	if err := storage.init(); err != nil {
		return nil, err
	}

	return storage, nil
}

// init инициализирует таблицы БД
func (s *WebStorage) init() error {
	// Читаем и выполняем миграцию
	migrationSQL := `
-- Web App Database Schema

-- Пользователи веб-приложения
CREATE TABLE if NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Аккаунты MEXC
CREATE TABLE if NOT EXISTS accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    NAME TEXT NOT NULL,
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
    UNIQUE(user_id, NAME),
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX if NOT EXISTS idx_accounts_user ON accounts(user_id);
CREATE INDEX if NOT EXISTS idx_accounts_master ON accounts(user_id, is_master);

-- История сделок (детальная)
CREATE TABLE if NOT EXISTS trades (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    master_account_id INTEGER,
    symbol TEXT NOT NULL,
    side INTEGER NOT NULL,
    volume INTEGER NOT NULL,
    leverage INTEGER NOT NULL,
    ACTION TEXT NOT NULL DEFAULT '',
    sent_at DATETIME NOT NULL,
    received_at DATETIME,
    exchange_accepted_at DATETIME,
    status TEXT NOT NULL,
    error TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY(master_account_id) REFERENCES accounts(id) ON DELETE SET NULL
);

CREATE INDEX if NOT EXISTS idx_trades_user ON trades(user_id);
CREATE INDEX if NOT EXISTS idx_trades_sent ON trades(sent_at DESC);
CREATE INDEX if NOT EXISTS idx_trades_status ON trades(status);

-- Детали по каждому slave аккаунту в сделке
CREATE TABLE if NOT EXISTS trade_details (
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

CREATE INDEX if NOT EXISTS idx_trade_details_trade ON trade_details(trade_id);
CREATE INDEX if NOT EXISTS idx_trade_details_account ON trade_details(account_id);

-- Лог активности
CREATE TABLE if NOT EXISTS activity_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER,
    LEVEL TEXT NOT NULL,
    ACTION TEXT NOT NULL,
    message TEXT NOT NULL,
    details TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX if NOT EXISTS idx_activity_log_user ON activity_log(user_id);
CREATE INDEX if NOT EXISTS idx_activity_log_created ON activity_log(created_at DESC);
CREATE INDEX if NOT EXISTS idx_activity_log_level ON activity_log(LEVEL);

-- Copy Trading Sessions
CREATE TABLE if NOT EXISTS copy_trading_sessions (
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

CREATE INDEX if NOT EXISTS idx_sessions_user ON copy_trading_sessions(user_id);
CREATE INDEX if NOT EXISTS idx_sessions_active ON copy_trading_sessions(is_active);
`

	_, err := s.db.Exec(migrationSQL)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	// Миграция: добавляем колонку action в trades если её нет
	_, _ = s.db.Exec(`ALTER TABLE trades ADD COLUMN action text NOT NULL DEFAULT ''`)

	s.logger.Info("✅ Web database initialized")

	return nil
}

// === User Management ===

// CreateUser создает нового пользователя
func (s *WebStorage) CreateUser(username, passwordHash string) (*models2.User, error) {
	result, err := s.db.Exec(`
		INSERT INTO users (username, password_hash)
		VALUES (?, ?)
	`, username, passwordHash)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	id, _ := result.LastInsertId()

	return &models2.User{
		ID:           int(id),
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
	}, nil
}

// GetUserByUsername получает пользователя по имени
func (s *WebStorage) GetUserByUsername(username string) (*models2.User, error) {
	var user models2.User

	err := s.db.QueryRow(`
		SELECT id, username, password_hash, created_at
		FROM users
		WHERE username = ?
	`, username).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetUserByID получает пользователя по ID
func (s *WebStorage) GetUserByID(id int) (*models2.User, error) {
	var user models2.User

	err := s.db.QueryRow(`
		SELECT id, username, password_hash, created_at
		FROM users
		WHERE id = ?
	`, id).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// === Account Management ===

// AccountExistsByMexcUID проверяет, существует ли аккаунт с таким MEXC UID
func (s *WebStorage) AccountExistsByMexcUID(userID int, mexcUID string) (bool, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT count(*) FROM accounts
		WHERE user_id = ? AND user_id_mexc = ?
	`, userID, mexcUID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// AddAccount добавляет аккаунт для пользователя
func (s *WebStorage) AddAccount(userID int, name string, data models2.BrowserData, proxy string) error {
	cookiesJSON, _ := json.Marshal(data.AllCookies)

	_, err := s.db.Exec(`
		INSERT INTO accounts (user_id, name, token, user_id_mexc, device_id, cookies, user_agent, proxy)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, userID, name, data.UcToken, data.UID, data.DeviceID, string(cookiesJSON), data.UserAgent, proxy)
	if err != nil {
		return fmt.Errorf("failed to add account: %w", err)
	}

	s.logger.Info("✅ Account added",
		slog.String("name", name),
		slog.Int("user_id", userID))

	return nil
}

// GetAccounts возвращает все аккаунты пользователя
func (s *WebStorage) GetAccounts(userID int) ([]models2.Account, error) {
	rows, err := s.db.Query(`
		SELECT id, name, token, user_id_mexc, device_id,
		       coalesce(cookies, '{}'), coalesce(user_agent, ''), coalesce(proxy, ''),
		       coalesce(is_master, 0), coalesce(disabled, 0)
		FROM accounts
		WHERE user_id = ?
		ORDER BY id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []models2.Account
	for rows.Next() {
		var acc models2.Account
		var cookiesJSON string
		var isMasterInt, disabledInt int

		err := rows.Scan(&acc.ID, &acc.Name, &acc.Token, &acc.UserID,
			&acc.DeviceID, &cookiesJSON, &acc.UserAgent, &acc.Proxy, &isMasterInt, &disabledInt)
		if err != nil {
			continue
		}

		json.Unmarshal([]byte(cookiesJSON), &acc.Cookies)
		acc.IsMaster = isMasterInt == 1
		acc.Disabled = disabledInt == 1
		accounts = append(accounts, acc)
	}

	return accounts, nil
}

// DeleteAccount удаляет аккаунт
func (s *WebStorage) DeleteAccount(userID int, accountID int) error {
	result, err := s.db.Exec("DELETE FROM accounts WHERE user_id = ? AND id = ?", userID, accountID)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("account not found")
	}

	s.logger.Info("✅ Account deleted",
		slog.Int("account_id", accountID),
		slog.Int("user_id", userID))

	return nil
}

// SetMasterAccount устанавливает аккаунт как главный
func (s *WebStorage) SetMasterAccount(userID int, accountID int) error {
	// Убираем флаг master у всех аккаунтов
	_, err := s.db.Exec("UPDATE accounts SET is_master = 0 WHERE user_id = ?", userID)
	if err != nil {
		return err
	}

	// Устанавливаем флаг для нужного аккаунта
	result, err := s.db.Exec("UPDATE accounts SET is_master = 1 WHERE user_id = ? AND id = ?", userID, accountID)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("account not found")
	}

	s.logger.Info("✅ Master account set",
		slog.Int("account_id", accountID),
		slog.Int("user_id", userID))

	return nil
}

// UpdateDisabledStatus обновляет disabled статус аккаунта
func (s *WebStorage) UpdateDisabledStatus(userID int, accountID int, disabled bool) error {
	disabledInt := 0
	if disabled {
		disabledInt = 1
	}

	result, err := s.db.Exec("UPDATE accounts SET disabled = ? WHERE user_id = ? AND id = ?", disabledInt, userID, accountID)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("account not found")
	}

	return nil
}

// UpdateAutoDisableOnFee обновляет auto_disable_on_fee статус аккаунта
func (s *WebStorage) UpdateAutoDisableOnFee(userID int, accountID int, autoDisable bool) error {
	autoDisableInt := 0
	if autoDisable {
		autoDisableInt = 1
	}

	result, err := s.db.Exec("UPDATE accounts SET auto_disable_on_fee = ? WHERE user_id = ? AND id = ?", autoDisableInt, userID, accountID)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("account not found")
	}

	return nil
}

// GetMasterAccount возвращает главный аккаунт
func (s *WebStorage) GetMasterAccount(userID int) (models2.Account, error) {
	var acc models2.Account
	var cookiesJSON string
	var isMasterInt, disabledInt int

	err := s.db.QueryRow(`
		SELECT id, name, token, user_id_mexc, device_id,
		       coalesce(cookies, '{}'), coalesce(user_agent, ''), coalesce(proxy, ''),
		       coalesce(is_master, 0), coalesce(disabled, 0)
		FROM accounts
		WHERE user_id = ? AND is_master = 1
		LIMIT 1
	`, userID).Scan(&acc.ID, &acc.Name, &acc.Token, &acc.UserID,
		&acc.DeviceID, &cookiesJSON, &acc.UserAgent, &acc.Proxy, &isMasterInt, &disabledInt)
	if err != nil {
		return models2.Account{}, err
	}

	json.Unmarshal([]byte(cookiesJSON), &acc.Cookies)
	acc.IsMaster = isMasterInt == 1
	acc.Disabled = disabledInt == 1

	return acc, nil
}

// GetSlaveAccounts возвращает все slave аккаунты
func (s *WebStorage) GetSlaveAccounts(userID int, includeDisabled bool) ([]models2.Account, error) {
	query := `
		SELECT id, name, token, user_id_mexc, device_id,
		       coalesce(cookies, '{}'), coalesce(user_agent, ''), coalesce(proxy, ''),
		       coalesce(is_master, 0), coalesce(disabled, 0)
		FROM accounts
		WHERE user_id = ? AND is_master = 0`

	if !includeDisabled {
		query += " AND disabled = 0"
	}

	query += " ORDER BY id"

	rows, err := s.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []models2.Account
	for rows.Next() {
		var acc models2.Account
		var cookiesJSON string
		var isMasterInt, disabledInt int

		err := rows.Scan(&acc.ID, &acc.Name, &acc.Token, &acc.UserID,
			&acc.DeviceID, &cookiesJSON, &acc.UserAgent, &acc.Proxy, &isMasterInt, &disabledInt)
		if err != nil {
			continue
		}

		json.Unmarshal([]byte(cookiesJSON), &acc.Cookies)
		acc.IsMaster = isMasterInt == 1
		acc.Disabled = disabledInt == 1
		accounts = append(accounts, acc)
	}

	return accounts, nil
}

// === Trades History ===

// CreateTrade создает новую запись сделки
func (s *WebStorage) CreateTrade(_ context.Context, trade models2.Trade) (int, error) {
	result, err := s.db.Exec(`
		INSERT INTO trades (user_id, master_account_id, symbol, side, volume, leverage, action, sent_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, trade.UserID, trade.MasterAccountID, trade.Symbol, trade.Side, trade.Volume, trade.Leverage, trade.Action, trade.SentAt, trade.Status)
	if err != nil {
		return 0, err
	}

	id, _ := result.LastInsertId()

	return int(id), nil
}

// UpdateTradeReceived обновляет время получения ответа
func (s *WebStorage) UpdateTradeReceived(tradeID int, receivedAt time.Time) error {
	_, err := s.db.Exec("UPDATE trades SET received_at = ? WHERE id = ?", receivedAt, tradeID)
	return err
}

// UpdateTradeStatus обновляет статус сделки
func (s *WebStorage) UpdateTradeStatus(_ context.Context, tradeID int, status string, errorMsg string) error {
	_, err := s.db.Exec("UPDATE trades SET status = ?, error = ? WHERE id = ?", status, errorMsg, tradeID)
	return err
}

// AddTradeDetail добавляет детали выполнения сделки на аккаунте
func (s *WebStorage) AddTradeDetail(_ context.Context, detail models2.TradeDetail) error {
	_, err := s.db.Exec(`
		INSERT INTO trade_details (trade_id, account_id, status, error, order_id, latency_ms)
		VALUES (?, ?, ?, ?, ?, ?)
	`, detail.TradeID, detail.AccountID, detail.Status, detail.Error, detail.OrderID, detail.LatencyMs)

	return err
}

// GetTrades получает историю сделок с пагинацией
func (s *WebStorage) GetTrades(userID int, limit, offset int) ([]models2.Trade, error) {
	rows, err := s.db.Query(`
		SELECT t.id, t.user_id, t.master_account_id, coalesce(a.name, ''), t.symbol, t.side, t.volume, t.leverage,
		       coalesce(t.action, ''), t.sent_at, t.received_at, t.exchange_accepted_at, t.status, coalesce(t.error, ''), t.created_at
		FROM trades t
		LEFT JOIN accounts a ON t.master_account_id = a.id
		WHERE t.user_id = ?
		ORDER BY t.sent_at DESC
		LIMIT ? OFFSET ?
	`, userID, limit, offset)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var trades []models2.Trade
	for rows.Next() {
		var trade models2.Trade
		err := rows.Scan(
			&trade.ID, &trade.UserID, &trade.MasterAccountID, &trade.MasterAccountName,
			&trade.Symbol, &trade.Side, &trade.Volume, &trade.Leverage,
			&trade.Action, &trade.SentAt, &trade.ReceivedAt, &trade.ExchangeAcceptedAt,
			&trade.Status, &trade.Error, &trade.CreatedAt,
		)
		if err != nil {
			continue
		}

		// Загружаем детали
		trade.Details, _ = s.GetTradeDetails(trade.ID)
		trades = append(trades, trade)
	}

	return trades, nil
}

// GetTradeDetails получает детали сделки
func (s *WebStorage) GetTradeDetails(tradeID int) ([]models2.TradeDetail, error) {
	rows, err := s.db.Query(`
		SELECT td.id, td.trade_id, td.account_id, a.name, td.status, coalesce(td.error, ''),
		       coalesce(td.order_id, ''), coalesce(td.latency_ms, 0), td.created_at
		FROM trade_details td
		LEFT JOIN accounts a ON td.account_id = a.id
		WHERE td.trade_id = ?
		ORDER BY td.id
	`, tradeID)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var details []models2.TradeDetail
	for rows.Next() {
		var detail models2.TradeDetail
		err := rows.Scan(
			&detail.ID, &detail.TradeID, &detail.AccountID, &detail.AccountName,
			&detail.Status, &detail.Error, &detail.OrderID, &detail.LatencyMs, &detail.CreatedAt,
		)
		if err != nil {
			continue
		}

		details = append(details, detail)
	}

	return details, nil
}

// === Activity Log ===

// AddLog добавляет запись в лог
func (s *WebStorage) AddLog(_ context.Context, log models2.ActivityLog) error {
	_, err := s.db.Exec(`
		INSERT INTO activity_log (user_id, level, action, message, details)
		VALUES (?, ?, ?, ?, ?)
	`, log.UserID, log.Level, log.Action, log.Message, log.Details)

	return err
}

// GetLogs получает логи с пагинацией
func (s *WebStorage) GetLogs(userID int, limit, offset int) ([]models2.ActivityLog, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, level, ACTION, message, COALESCE(details, ''), created_at
		FROM activity_log
		WHERE user_id = ? OR user_id IS NULL
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, userID, limit, offset)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var logs []models2.ActivityLog
	for rows.Next() {
		var log models2.ActivityLog
		err := rows.Scan(
			&log.ID, &log.UserID, &log.Level, &log.Action, &log.Message, &log.Details, &log.CreatedAt,
		)
		if err != nil {
			continue
		}

		logs = append(logs, log)
	}

	return logs, nil
}

// === Copy Trading Sessions ===

// HasActiveCopyTradingSession проверяет, есть ли активная сессия copy trading
func (s *WebStorage) HasActiveCopyTradingSession(userID int) (bool, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT count(*) FROM copy_trading_sessions
		WHERE user_id = ? AND is_active = 1
	`, userID).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// Close закрывает соединение с БД
func (s *WebStorage) Close() error {
	return s.db.Close()
}
