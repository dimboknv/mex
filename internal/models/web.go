package models

import "time"

// User представляет пользователя веб-приложения
type User struct {
	ID           int
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

// Trade представляет сделку в истории
type Trade struct {
	ID                 int           `json:"id"`
	UserID             int           `json:"user_id"`
	MasterAccountID    *int          `json:"master_account_id,omitempty"`
	MasterAccountName  string        `json:"master_account_name,omitempty"` // Joined field
	Symbol             string        `json:"symbol"`
	Side               int           `json:"side"`
	Volume             int           `json:"volume"`
	Leverage           int           `json:"leverage"`
	Action             string        `json:"action"` // "open_position", "close_position", "change_leverage", etc.
	SentAt             time.Time     `json:"sent_at"`
	ReceivedAt         *time.Time    `json:"received_at,omitempty"`
	ExchangeAcceptedAt *time.Time    `json:"exchange_accepted_at,omitempty"`
	Status             string        `json:"status"` // "success", "partial", "failed"
	Error              string        `json:"error,omitempty"`
	CreatedAt          time.Time     `json:"created_at"`
	Details            []TradeDetail `json:"details,omitempty"` // Joined field
}

// TradeDetail представляет детали выполнения сделки на конкретном аккаунте
type TradeDetail struct {
	ID          int       `json:"id"`
	TradeID     int       `json:"trade_id"`
	AccountID   int       `json:"account_id"`
	AccountName string    `json:"account_name,omitempty"` // Joined field
	Status      string    `json:"status"`                 // "success", "failed"
	Error       string    `json:"error,omitempty"`
	OrderID     string    `json:"order_id,omitempty"`
	LatencyMs   int       `json:"latency_ms"`
	CreatedAt   time.Time `json:"created_at"`
}

// ActivityLog представляет запись в логе активности
type ActivityLog struct {
	ID        int       `json:"id"`
	UserID    *int      `json:"user_id,omitempty"`
	Level     string    `json:"level"`  // "INFO", "WARN", "ERROR"
	Action    string    `json:"action"` // "copy_trading_started", "order_placed", etc.
	Message   string    `json:"message"`
	Details   string    `json:"details,omitempty"` // JSON с дополнительной информацией
	CreatedAt time.Time `json:"created_at"`
}

// CopyTradingSession представляет сессию copy trading
type CopyTradingSession struct {
	ID               int
	UserID           int
	MasterAccountID  int
	IsActive         bool
	IgnoreFees       bool
	StartedAt        time.Time
	StoppedAt        *time.Time
	MasterAccount    *Account // Joined field
	ActiveSlaveCount int      // Computed field
}

// AccountWithFee расширяет Account дополнительной информацией о комиссиях
type AccountWithFee struct {
	Account
	MakerFee float64
	TakerFee float64
	Balance  float64
}

// DashboardStats статистика для дашборда
type DashboardStats struct {
	TotalAccounts     int
	ActiveAccounts    int
	DisabledAccounts  int
	MasterAccount     *Account
	CopyTradingActive bool
	TodayTrades       int
	TodaySuccessRate  float64
	TotalBalance      float64
	RecentErrors      []ActivityLog
}
