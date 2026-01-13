package api

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"tg_mexc/pkg/services/copytrading"
	wscopytrading "tg_mexc/pkg/services/copytrading/websocekt"
)

type wsCopyTradingManager struct {
	connections map[int]*wscopytrading.Service
	mu          sync.RWMutex
	logger      *slog.Logger
	manager     *copytrading.Manager
}

func (m *wsCopyTradingManager) starSession(userID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, err := m.manager.CreateOrGetActiveSession(userID, "websocket")
	if err != nil {
		return fmt.Errorf("failed to start copy trading: %w", err)
	}

	wsService := wscopytrading.NewService(session, m.logger)

	if err := wsService.Start(); err != nil {
		return errors.Join(fmt.Errorf("failed to start copy trading: %w", err), m.manager.StopSession(userID, "websocket"))
	}

	m.connections[userID] = wsService

	return nil
}

func (m *wsCopyTradingManager) stopSession(userID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wsService, ok := m.connections[userID]
	if !ok {
		return nil
	}

	if err := errors.Join(wsService.Stop(), m.manager.StopSession(userID, "websocket")); err != nil {
		return err
	}

	delete(m.connections, userID)

	return nil
}

func (m *wsCopyTradingManager) isActive(userID int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.connections[userID]
	return ok
}
