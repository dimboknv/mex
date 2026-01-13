package copytrading

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	models2 "tg_mexc/internal/models"
)

type Session struct {
	userID int
	active bool
	engine *Engine
	name   string
	mu     sync.RWMutex
}

func (s *Session) isActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *Session) ensureActive() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return errors.New("session is not active")
	}

	return nil
}

func (s *Session) GetMasterAccount() (models2.Account, error) {
	return s.engine.userStorage.GetMasterAccount(s.userID)
}

func (s *Session) OpenPosition(ctx context.Context, req OpenPositionRequest) (ExecutionResult, error) {
	return s.execute(func() (ExecutionResult, error) {
		return s.engine.OpenPosition(ctx, s.userID, req)
	})
}

func (s *Session) ClosePosition(ctx context.Context, req ClosePositionRequest) (ExecutionResult, error) {
	return s.execute(func() (ExecutionResult, error) {
		return s.engine.ClosePosition(ctx, s.userID, req)
	})
}

func (s *Session) PlacePlanOrder(ctx context.Context, req PlacePlanOrderRequest) (ExecutionResult, error) {
	return s.execute(func() (ExecutionResult, error) {
		return s.engine.PlacePlanOrder(ctx, s.userID, req)
	})
}

func (s *Session) ChangePlanPrice(ctx context.Context, req ChangePlanPriceRequest) (ExecutionResult, error) {
	return s.execute(func() (ExecutionResult, error) {
		return s.engine.ChangePlanPrice(ctx, s.userID, req)
	})
}

func (s *Session) ChangeLeverage(ctx context.Context, req ChangeLeverageRequest) (ExecutionResult, error) {
	return s.execute(func() (ExecutionResult, error) {
		return s.engine.ChangeLeverage(ctx, s.userID, req)
	})
}

func (s *Session) CancelStopOrder(ctx context.Context, orderIDs []int) (ExecutionResult, error) {
	return s.execute(func() (ExecutionResult, error) {
		return s.engine.CancelStopOrder(ctx, s.userID, orderIDs)
	})
}

func (s *Session) CancelStopOrderBySymbol(ctx context.Context, symbol string) (ExecutionResult, error) {
	return s.execute(func() (ExecutionResult, error) {
		return s.engine.CancelStopOrderBySymbol(ctx, s.userID, symbol)
	})
}

func (s *Session) execute(fn func() (ExecutionResult, error)) (ExecutionResult, error) {
	if err := s.ensureActive(); err != nil {
		return ExecutionResult{}, err
	}

	return fn()
}

type Manager struct {
	logger *slog.Logger
	dryRun bool
	engine *Engine

	mu       sync.Mutex
	sessions map[int]*Session
}

func NewManager(
	engine *Engine,
	dryRun bool,
	logger *slog.Logger,
) *Manager {
	return &Manager{
		logger:   logger,
		dryRun:   dryRun,
		engine:   engine,
		sessions: make(map[int]*Session),
	}
}

func (m *Manager) IsDryRun() bool {
	return m.dryRun
}

func (m *Manager) StopAllSessions() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, session := range m.sessions {
		session.active = false
	}

	m.sessions = make(map[int]*Session)
	return
}

func (m *Manager) CreateOrGetActiveSession(userID int, name string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[userID]
	if ok {
		if session.name != name {
			return nil, fmt.Errorf("session already started with another name")
		}

		if !session.isActive() {
			return nil, fmt.Errorf("session is not active")
		}

		return session, nil
	}

	session, err := m.startSession(userID, name)
	if err != nil {
		return nil, err
	}

	return session, nil
}

func (m *Manager) GetSession(userID int, name string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[userID]
	if ok {
		if session.name != name {
			return nil, fmt.Errorf("session already started with another name")
		}

		if !session.isActive() {
			return nil, fmt.Errorf("session is not active")
		}

		return session, nil
	}

	return nil, fmt.Errorf("session not found")
}

func (m *Manager) startSession(userID int, name string) (*Session, error) {
	session, ok := m.sessions[userID]
	if ok {
		if session.name != name {
			return nil, fmt.Errorf("session already started with another name")
		}

		if session.isActive() {
			return nil, fmt.Errorf("session already started")
		}

		return session, nil
	}

	session = &Session{
		userID: userID,
		engine: m.engine,
		name:   name,
		active: true,
	}

	m.sessions[userID] = session

	// Логируем старт сессии
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.engine.logStorage.AddLog(ctx, models2.ActivityLog{
		UserID:  &userID,
		Level:   "info",
		Action:  "copy_trading_start",
		Message: fmt.Sprintf("Copy trading session started (mode: %s)", name),
	})

	m.logger.Info("Copy trading session started",
		slog.Int("user_id", userID),
		slog.String("mode", name),
		slog.Bool("dry_run", m.dryRun))

	return session, nil
}

func (m *Manager) StopSession(userID int, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[userID]
	if !ok {
		return fmt.Errorf("session not found")
	}

	session.active = false

	delete(m.sessions, userID)

	// Логируем остановку сессии
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.engine.logStorage.AddLog(ctx, models2.ActivityLog{
		UserID:  &userID,
		Level:   "info",
		Action:  "copy_trading_stop",
		Message: fmt.Sprintf("Copy trading session stopped (mode: %s)", name),
	})

	m.logger.Info("Copy trading session stopped",
		slog.Int("user_id", userID),
		slog.String("mode", name))

	return nil
}
