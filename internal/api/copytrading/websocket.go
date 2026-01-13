package copytrading

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	corecopytrade "tg_mexc/internal/mexc/copytrading"
	"tg_mexc/internal/mexc/copytrading/websocket"
	"tg_mexc/internal/models"
)

// AccountStorage - интерфейс для получения аккаунтов
type AccountStorage interface {
	GetMasterAccount(userID int) (models.Account, error)
	GetSlaveAccounts(userID int, includeDisabled bool) ([]models.Account, error)
}

// webSocketService реализует WebSocketService
type webSocketService struct {
	manager     *corecopytrade.Manager
	storage     AccountStorage
	logger      *slog.Logger
	connections map[int]*wscopytrading.Service
	mu          sync.RWMutex
}

// NewWebSocketService создаёт новый WebSocket сервис
func NewWebSocketService(
	manager *corecopytrade.Manager,
	storage AccountStorage,
	logger *slog.Logger,
) WebSocketService {
	return &webSocketService{
		manager:     manager,
		storage:     storage,
		logger:      logger,
		connections: make(map[int]*wscopytrading.Service),
	}
}

func (s *webSocketService) Start(ctx context.Context, userID int, opts ModeOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Проверяем что есть master аккаунт
	if _, err := s.storage.GetMasterAccount(userID); err != nil {
		return fmt.Errorf("master account not set: %w", err)
	}

	// Создаём сессию в manager
	session, err := s.manager.CreateOrGetActiveSession(userID, "websocket")
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	// Создаём WebSocket сервис
	wsService := wscopytrading.NewService(session, s.logger)

	if err := wsService.Start(); err != nil {
		_ = s.manager.StopSession(userID, "websocket")
		return fmt.Errorf("failed to start websocket: %w", err)
	}

	s.connections[userID] = wsService

	s.logger.Info("WebSocket copy trading started",
		slog.Int("user_id", userID),
		slog.Bool("ignore_fees", opts.IgnoreFees))

	return nil
}

func (s *webSocketService) Stop(ctx context.Context, userID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	wsService, ok := s.connections[userID]
	if !ok {
		return nil
	}

	var errs []error

	if err := wsService.Stop(); err != nil {
		errs = append(errs, fmt.Errorf("failed to stop websocket: %w", err))
	}

	if err := s.manager.StopSession(userID, "websocket"); err != nil {
		errs = append(errs, fmt.Errorf("failed to stop session: %w", err))
	}

	delete(s.connections, userID)

	s.logger.Info("WebSocket copy trading stopped", slog.Int("user_id", userID))

	return errors.Join(errs...)
}

func (s *webSocketService) IsActive(userID int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.connections[userID]
	return ok
}

func (s *webSocketService) stopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for userID, wsService := range s.connections {
		_ = wsService.Stop()
		_ = s.manager.StopSession(userID, "websocket")
		s.logger.Info("WebSocket stopped (shutdown)", slog.Int("user_id", userID))
	}

	s.connections = make(map[int]*wscopytrading.Service)
}
