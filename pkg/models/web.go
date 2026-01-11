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
	ID                 int
	UserID             int
	MasterAccountID    *int
	MasterAccountName  string // Joined field
	Symbol             string
	Side               int
	Volume             int
	Leverage           int
	SentAt             time.Time
	ReceivedAt         *time.Time
	ExchangeAcceptedAt *time.Time
	Status             string // "success", "partial", "failed"
	Error              string
	CreatedAt          time.Time
	Details            []TradeDetail // Joined field
}

// TradeDetail представляет детали выполнения сделки на конкретном аккаунте
type TradeDetail struct {
	ID          int
	TradeID     int
	AccountID   int
	AccountName string // Joined field
	Status      string // "success", "failed"
	Error       string
	OrderID     string
	LatencyMs   int
	CreatedAt   time.Time
}

// ActivityLog представляет запись в логе активности
type ActivityLog struct {
	ID        int
	UserID    *int
	Level     string // "INFO", "WARN", "ERROR"
	Action    string // "copy_trading_started", "order_placed", etc.
	Message   string
	Details   string // JSON с дополнительной информацией
	CreatedAt time.Time
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
