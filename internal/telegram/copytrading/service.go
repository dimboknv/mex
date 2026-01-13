package telegramcopytrading

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"tg_mexc/internal/mexc/copytrading"
	wscopytrading "tg_mexc/internal/mexc/copytrading/websocket"
	"tg_mexc/internal/models"
	"tg_mexc/internal/storage"
)

// Service - адаптер copy trading для Telegram бота
// Конвертирует chatID в userID и управляет WebSocket сессиями
type Service struct {
	manager *copytrading.Manager
	storage *storage.WebStorage
	logger  *slog.Logger

	mu       sync.RWMutex
	sessions map[int64]*telegramSession // chatID -> session
}

type telegramSession struct {
	userID     int
	wsService  *wscopytrading.Service
	eventChan  chan string
	ignoreFees bool
}

// New создает новый Telegram copy trading сервис
func New(
	manager *copytrading.Manager,
	storage *storage.WebStorage,
	logger *slog.Logger,
) *Service {
	return &Service{
		manager:  manager,
		storage:  storage,
		logger:   logger,
		sessions: make(map[int64]*telegramSession),
	}
}

// Start запускает copy trading для Telegram чата
func (s *Service) Start(chatID int64, ignoreFees bool) (string, error) {
	// Получаем или создаем пользователя
	userID, err := s.storage.GetOrCreateUserByTelegramChatID(chatID)
	if err != nil {
		return "", fmt.Errorf("не удалось определить пользователя: %w", err)
	}

	// Проверяем, что есть мастер аккаунт
	master, err := s.storage.GetMasterAccount(userID)
	if err != nil {
		return "", fmt.Errorf("мастер аккаунт не установлен. Используй /set_master <name>")
	}

	// Проверяем, что есть slave аккаунты
	slaves, err := s.storage.GetSlaveAccounts(userID, ignoreFees)
	if err != nil {
		return "", fmt.Errorf("ошибка получения slave аккаунтов: %w", err)
	}

	if len(slaves) == 0 {
		return "", fmt.Errorf("нет активных slave аккаунтов для копирования")
	}

	// Проверяем, не запущена ли уже сессия
	s.mu.Lock()
	if _, ok := s.sessions[chatID]; ok {
		s.mu.Unlock()
		return "", fmt.Errorf("copy trading уже запущен")
	}
	s.mu.Unlock()

	// Создаем сессию в менеджере
	session, err := s.manager.CreateOrGetActiveSession(userID, "websocket")
	if err != nil {
		return "", fmt.Errorf("не удалось создать сессию: %w", err)
	}

	// Создаем WebSocket сервис
	wsService := wscopytrading.NewService(session, s.logger)
	if err := wsService.Start(); err != nil {
		s.manager.StopSession(userID, "websocket")
		return "", fmt.Errorf("ошибка WebSocket подключения: %w", err)
	}

	// Создаем канал для событий
	eventChan := make(chan string, 100)

	// Сохраняем сессию
	s.mu.Lock()
	s.sessions[chatID] = &telegramSession{
		userID:     userID,
		wsService:  wsService,
		eventChan:  eventChan,
		ignoreFees: ignoreFees,
	}
	s.mu.Unlock()

	s.logger.Info("Copy trading started for Telegram",
		slog.Int64("chat_id", chatID),
		slog.Int("user_id", userID),
		slog.String("master", master.Name),
		slog.Int("slaves", len(slaves)),
		slog.Bool("ignore_fees", ignoreFees),
		slog.Bool("dry_run", s.manager.IsDryRun()))

	dryRunInfo := ""
	if s.manager.IsDryRun() {
		dryRunInfo = "\n\n⚠️ DRY RUN режим: сделки не будут реально открываться"
	}

	return fmt.Sprintf(`✅ Copy Trading запущен!

👑 Мастер: %s
📊 Slave аккаунтов: %d
🔄 Ignore fees: %v%s`,
		master.Name, len(slaves), ignoreFees, dryRunInfo), nil
}

// Stop останавливает copy trading для Telegram чата
func (s *Service) Stop(chatID int64) (string, error) {
	s.mu.Lock()
	session, ok := s.sessions[chatID]
	if !ok {
		s.mu.Unlock()
		return "", fmt.Errorf("copy trading не активен")
	}
	delete(s.sessions, chatID)
	s.mu.Unlock()

	// Останавливаем WebSocket
	if err := session.wsService.Stop(); err != nil {
		s.logger.Error("Error stopping WebSocket", slog.Any("error", err))
	}

	// Останавливаем сессию в менеджере
	s.manager.StopSession(session.userID, "websocket")

	// Закрываем канал событий
	close(session.eventChan)

	s.logger.Info("Copy trading stopped for Telegram",
		slog.Int64("chat_id", chatID),
		slog.Int("user_id", session.userID))

	return "✅ Copy Trading остановлен", nil
}

// IsActive проверяет, активен ли copy trading
func (s *Service) IsActive(chatID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.sessions[chatID]
	return ok
}

// GetStatus возвращает статус copy trading
func (s *Service) GetStatus(chatID int64) string {
	s.mu.RLock()
	session, ok := s.sessions[chatID]
	s.mu.RUnlock()

	if !ok {
		return "📊 Copy Trading: ❌ ОСТАНОВЛЕН"
	}

	master, err := s.storage.GetMasterAccount(session.userID)
	if err != nil {
		return "📊 Copy Trading: ✅ АКТИВЕН\n❌ Ошибка получения мастера"
	}

	slaves, _ := s.storage.GetSlaveAccounts(session.userID, session.ignoreFees)

	dryRunInfo := ""
	if s.manager.IsDryRun() {
		dryRunInfo = "\n⚠️ DRY RUN режим"
	}

	return fmt.Sprintf(`📊 Copy Trading: ✅ АКТИВЕН

👑 Мастер: %s
📊 Slave аккаунтов: %d
🔄 Ignore fees: %v%s`,
		master.Name, len(slaves), session.ignoreFees, dryRunInfo)
}

// GetEventChannel возвращает канал событий для чата
func (s *Service) GetEventChannel(chatID int64) <-chan string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[chatID]
	if !ok {
		return nil
	}
	return session.eventChan
}

// StopAll останавливает все сессии (для graceful shutdown)
func (s *Service) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for chatID, session := range s.sessions {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = ctx

		if err := session.wsService.Stop(); err != nil {
			s.logger.Error("Error stopping WebSocket", slog.Any("error", err))
		}
		s.manager.StopSession(session.userID, "websocket")
		close(session.eventChan)

		s.logger.Info("Copy trading stopped",
			slog.Int64("chat_id", chatID))

		cancel()
	}

	s.sessions = make(map[int64]*telegramSession)
}

// SendEvent отправляет событие в канал (для уведомлений о сделках)
func (s *Service) SendEvent(chatID int64, message string) {
	s.mu.RLock()
	session, ok := s.sessions[chatID]
	s.mu.RUnlock()

	if !ok {
		return
	}

	select {
	case session.eventChan <- message:
	default:
		// Канал переполнен, пропускаем
		s.logger.Warn("Event channel full, dropping message",
			slog.Int64("chat_id", chatID))
	}
}

// GetMasterAccount возвращает мастер аккаунт для чата
func (s *Service) GetMasterAccount(chatID int64) (*models.Account, error) {
	userID, err := s.storage.GetOrCreateUserByTelegramChatID(chatID)
	if err != nil {
		return nil, err
	}

	master, err := s.storage.GetMasterAccount(userID)
	if err != nil {
		return nil, err
	}

	return &master, nil
}
