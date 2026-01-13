package copytrading

import "context"

// Mode - режим copy trading
type Mode string

const (
	ModeOff       Mode = "off"
	ModeWebSocket Mode = "websocket"
	ModeMirror    Mode = "mirror"
)

// ModeOptions - опции для режима
type ModeOptions struct {
	IgnoreFees bool `json:"ignore_fees"` // только для websocket
}

// WebSocketService управляет WebSocket режимом copy trading
type WebSocketService interface {
	Start(ctx context.Context, userID int, opts ModeOptions) error
	Stop(ctx context.Context, userID int) error
	IsActive(userID int) bool
}

// MirrorService управляет Mirror режимом copy trading
type MirrorService interface {
	Start(ctx context.Context, userID int, username string) (token string, err error)
	Stop(ctx context.Context, userID int) error
	IsActive(userID int) bool
	ProcessRequest(ctx context.Context, token string, path string, body []byte) error
	ValidateToken(token string) (userID int, username string, ok bool)
	GetToken(userID int, username string) string
}

// CopyTradingService - главный сервис для управления copy trading
type CopyTradingService interface {
	// SetMode переключает режим copy trading (останавливает текущий, запускает новый)
	SetMode(ctx context.Context, userID int, username string, mode Mode, opts ModeOptions) error
	// GetStatus возвращает текущий статус
	GetStatus(ctx context.Context, userID int, username string) Status
	// StopAll останавливает все сессии (для graceful shutdown)
	StopAll()
	// GetMirrorScript возвращает JS скрипт для mirror режима
	GetMirrorScript(userID int, username string) string
	// ValidateMirrorToken валидирует токен mirror режима
	ValidateMirrorToken(token string) (userID int, username string, ok bool)
	// ProcessMirrorRequest обрабатывает запрос от mirror
	ProcessMirrorRequest(ctx context.Context, token string, path string, body []byte) error
}

// Status - статус copy trading
type Status struct {
	Mode             Mode   `json:"mode"`
	MasterName       string `json:"master_name,omitempty"`
	ActiveSlaveCount int    `json:"active_slave_count"`
	DryRun           bool   `json:"dry_run"`
	// Mirror-specific
	MirrorToken  string `json:"mirror_token,omitempty"`
	MirrorURL    string `json:"mirror_url,omitempty"`
	MirrorScript string `json:"mirror_script,omitempty"`
}
