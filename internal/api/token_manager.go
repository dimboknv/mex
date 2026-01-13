package api

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"
)

// mirrorToken - токен для идентификации пользователя
type mirrorToken struct {
	Token     string
	UserID    int
	Username  string
	CreatedAt time.Time
}

// mirrorTokenManager управляет mirror токенами и сессиями
type mirrorTokenManager struct {
	tokens map[string]*mirrorToken
	mu     sync.RWMutex
	logger *slog.Logger
}

// newMirrorTokenManager создает новый менеджер
func newMirrorTokenManager(logger *slog.Logger) *mirrorTokenManager {
	return &mirrorTokenManager{
		tokens: make(map[string]*mirrorToken),
		logger: logger,
	}
}

// GenerateToken создает новый токен для пользователя
func (m *mirrorTokenManager) GenerateToken(userID int, username string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Удаляем старый токен если есть
	for token, mt := range m.tokens {
		if mt.UserID == userID {
			delete(m.tokens, token)
			break
		}
	}

	// Генерируем новый токен
	bytes := make([]byte, 16)
	rand.Read(bytes)
	token := hex.EncodeToString(bytes)

	m.tokens[token] = &mirrorToken{
		Token:     token,
		UserID:    userID,
		Username:  username,
		CreatedAt: time.Now(),
	}

	return token
}

// ValidateToken проверяет токен и возвращает данные пользователя
func (m *mirrorTokenManager) ValidateToken(token string) (*mirrorToken, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mt, ok := m.tokens[token]

	return mt, ok
}

// GetTokenForUser возвращает токен для пользователя (или генерирует новый если нет)
func (m *mirrorTokenManager) GetTokenForUser(userID int, username string) string {
	m.mu.RLock()
	for _, mt := range m.tokens {
		if mt.UserID == userID {
			m.mu.RUnlock()
			return mt.Token
		}
	}
	m.mu.RUnlock()

	// Токена нет, генерируем новый
	return m.GenerateToken(userID, username)
}
