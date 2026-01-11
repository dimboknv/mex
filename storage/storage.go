package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"tg_mexc/models"

	_ "modernc.org/sqlite"
)

// Storage управляет базой данных
type Storage struct {
	db     *sql.DB
	logger *slog.Logger
}

// New создает новый экземпляр Storage
func New(dbPath string, logger *slog.Logger) (*Storage, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	storage := &Storage{
		db:     db,
		logger: logger,
	}

	if err := storage.init(); err != nil {
		return nil, err
	}

	return storage, nil
}

// init инициализирует таблицы БД
func (s *Storage) init() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			token TEXT NOT NULL,
			user_id TEXT NOT NULL,
			device_id TEXT NOT NULL,
			cookies TEXT,
			user_agent TEXT,
			proxy TEXT,
			is_master INTEGER DEFAULT 0,
			disabled INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(chat_id, name)
		);

		CREATE INDEX IF NOT EXISTS idx_chat_id ON accounts(chat_id);

		CREATE TABLE IF NOT EXISTS trades_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id INTEGER NOT NULL,
			symbol TEXT,
			side INTEGER,
			volume REAL,
			leverage INTEGER,
			success_count INTEGER,
			failed_count INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	// Миграция: добавляем колонку disabled если её нет
	s.db.Exec(`ALTER TABLE accounts ADD COLUMN disabled INTEGER DEFAULT 0`)

	s.logger.Info("✅ Database initialized")

	return nil
}

// AddAccount добавляет аккаунт для конкретного chat_id
func (s *Storage) AddAccount(chatID int64, name string, data models.BrowserData, proxy string) error {
	cookiesJSON, _ := json.Marshal(data.AllCookies)

	_, err := s.db.Exec(`
		INSERT INTO accounts (chat_id, name, token, user_id, device_id, cookies, user_agent, proxy)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, chatID, name, data.UcToken, data.UID, data.DeviceID, string(cookiesJSON), data.UserAgent, proxy)
	if err != nil {
		return fmt.Errorf("failed to add account: %w", err)
	}

	s.logger.Info("✅ Account added",
		slog.String("name", name),
		slog.Int64("chat_id", chatID))

	return nil
}

// GetAccounts возвращает все аккаунты для конкретного chat_id
func (s *Storage) GetAccounts(chatID int64) ([]models.Account, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, name, token, user_id, device_id,
		       COALESCE(cookies, '{}'), COALESCE(user_agent, ''), COALESCE(proxy, ''),
		       COALESCE(is_master, 0), COALESCE(disabled, 0)
		FROM accounts
		WHERE chat_id = ?
		ORDER BY id
	`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []models.Account
	for rows.Next() {
		var acc models.Account
		var cookiesJSON string
		var isMasterInt, disabledInt int

		err := rows.Scan(&acc.ID, &acc.ChatID, &acc.Name, &acc.Token, &acc.UserID,
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

// DeleteAccount удаляет аккаунт для конкретного chat_id
func (s *Storage) DeleteAccount(chatID int64, name string) error {
	result, err := s.db.Exec("DELETE FROM accounts WHERE chat_id = ? AND name = ?", chatID, name)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("аккаунт %s не найден", name)
	}

	s.logger.Info("✅ Account deleted",
		slog.String("name", name),
		slog.Int64("chat_id", chatID))

	return nil
}

// SetMasterAccount устанавливает аккаунт как главный (и убирает флаг у остальных)
func (s *Storage) SetMasterAccount(chatID int64, name string) error {
	// Убираем флаг master у всех аккаунтов
	_, err := s.db.Exec("UPDATE accounts SET is_master = 0 WHERE chat_id = ?", chatID)
	if err != nil {
		return err
	}

	// Устанавливаем флаг для нужного аккаунта
	result, err := s.db.Exec("UPDATE accounts SET is_master = 1 WHERE chat_id = ? AND name = ?", chatID, name)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("аккаунт %s не найден", name)
	}

	s.logger.Info("✅ Master account set",
		slog.String("name", name),
		slog.Int64("chat_id", chatID))

	return nil
}

// GetMasterAccount возвращает главный аккаунт
func (s *Storage) GetMasterAccount(chatID int64) (*models.Account, error) {
	var acc models.Account
	var cookiesJSON string
	var isMasterInt, disabledInt int

	err := s.db.QueryRow(`
		SELECT id, chat_id, name, token, user_id, device_id,
		       COALESCE(cookies, '{}'), COALESCE(user_agent, ''), COALESCE(proxy, ''),
		       COALESCE(is_master, 0), COALESCE(disabled, 0)
		FROM accounts
		WHERE chat_id = ? AND is_master = 1
		LIMIT 1
	`, chatID).Scan(&acc.ID, &acc.ChatID, &acc.Name, &acc.Token, &acc.UserID,
		&acc.DeviceID, &cookiesJSON, &acc.UserAgent, &acc.Proxy, &isMasterInt, &disabledInt)
	if err != nil {
		return nil, err
	}

	json.Unmarshal([]byte(cookiesJSON), &acc.Cookies)
	acc.IsMaster = isMasterInt == 1
	acc.Disabled = disabledInt == 1

	return &acc, nil
}

// GetSlaveAccounts возвращает все дочерние аккаунты (не мастер)
func (s *Storage) GetSlaveAccounts(chatID int64) ([]models.Account, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, name, token, user_id, device_id,
		       COALESCE(cookies, '{}'), COALESCE(user_agent, ''), COALESCE(proxy, ''),
		       COALESCE(is_master, 0), COALESCE(disabled, 0)
		FROM accounts
		WHERE chat_id = ? AND is_master = 0
		ORDER BY id
	`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []models.Account
	for rows.Next() {
		var acc models.Account
		var cookiesJSON string
		var isMasterInt, disabledInt int

		err := rows.Scan(&acc.ID, &acc.ChatID, &acc.Name, &acc.Token, &acc.UserID,
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

// UpdateDisabledStatus обновляет disabled статус аккаунта
func (s *Storage) UpdateDisabledStatus(chatID int64, name string, disabled bool) error {
	disabledInt := 0
	if disabled {
		disabledInt = 1
	}

	result, err := s.db.Exec("UPDATE accounts SET disabled = ? WHERE chat_id = ? AND name = ?", disabledInt, chatID, name)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("аккаунт %s не найден", name)
	}

	s.logger.Debug("Disabled status updated",
		slog.String("name", name),
		slog.Int64("chat_id", chatID),
		slog.Bool("disabled", disabled))

	return nil
}

// Close закрывает соединение с БД
func (s *Storage) Close() error {
	return s.db.Close()
}
