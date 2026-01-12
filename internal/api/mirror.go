package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"tg_mexc/internal/middleware"
	"tg_mexc/pkg/models"
	"tg_mexc/pkg/services/mexc"
)

// Подавление предупреждения о неиспользуемых импортах
var _ = models.Account{}

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
	Active    bool // состояние активности mirror mode
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

// SetActive устанавливает состояние активности для пользователя
func (m *MirrorManager) SetActive(userID int, active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, mt := range m.tokens {
		if mt.UserID == userID {
			mt.Active = active
			m.logger.Info("Mirror mode state changed",
				slog.Int("user_id", userID),
				slog.Bool("active", active))
			return
		}
	}
}

// IsActive проверяет, активен ли mirror mode для пользователя
func (m *MirrorManager) IsActive(userID int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, mt := range m.tokens {
		if mt.UserID == userID {
			return mt.Active
		}
	}
	return false
}

// GetTokenForUser возвращает токен для пользователя (или генерирует новый если нет)
func (m *MirrorManager) GetTokenForUser(userID int, username string) string {
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

// HandleMirrorReceive принимает перехваченные запросы (старый формат - JSON wrapper)
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

	h.respondSuccess(w, "OK", nil)
}

// HandleMirrorAPI обрабатывает прямые API запросы от browser mirror
func (h *Handler) HandleMirrorAPI(w http.ResponseWriter, r *http.Request) {
	// Получаем токен из header
	token := r.Header.Get("X-Mirror-Token")
	if token == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Валидируем токен
	mirrorToken, ok := h.mirrorManager.ValidateToken(token)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Проверяем, активен ли mirror mode для этого пользователя
	if !mirrorToken.Active {
		h.logger.Debug("Mirror request ignored - mirror mode not active",
			slog.Int("user_id", mirrorToken.UserID))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true,"ignored":true}`))
		return
	}

	// Читаем тело запроса
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("Failed to read request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Определяем тип запроса по URL path
	path := r.URL.Path

	h.logger.Info("🔵 Mirror API request",
		slog.String("user", mirrorToken.Username),
		slog.Int("user_id", mirrorToken.UserID),
		slog.String("path", path),
		slog.String("body", string(body)),
	)

	// Запускаем обработку в горутине и сразу отвечаем 200 OK
	go h.processMirrorRequest(mirrorToken.UserID, path, body)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success":true}`))
}

// processMirrorRequest обрабатывает mirror запрос и выполняет его на slave аккаунтах
func (h *Handler) processMirrorRequest(userID int, path string, body []byte) {
	ctx := context.Background()

	// Получаем slave аккаунты
	slaves, err := h.storage.GetSlaveAccounts(userID, false)
	if err != nil {
		h.logger.Error("Failed to get slave accounts",
			slog.Int("user_id", userID),
			slog.Any("error", err))
		return
	}

	if len(slaves) == 0 {
		h.logger.Info("No slave accounts found",
			slog.Int("user_id", userID))
		return
	}

	h.logger.Info("🚀 Processing mirror request for slaves",
		slog.Int("user_id", userID),
		slog.String("path", path),
		slog.Int("slave_count", len(slaves)))

	// Обрабатываем запрос в зависимости от пути
	switch {
	case strings.HasSuffix(path, "/order/create"):
		h.mirrorOrderCreate(ctx, slaves, body)
	case strings.HasSuffix(path, "/planorder/place"):
		h.mirrorPlanOrderPlace(ctx, slaves, body)
	case strings.HasSuffix(path, "/stoporder/cancel"):
		h.mirrorStopOrderCancel(ctx, slaves, body)
	case strings.HasSuffix(path, "/stoporder/change_plan_price"):
		h.mirrorChangePlanPrice(ctx, slaves, body)
	// case strings.HasSuffix(path, "/change_leverage"):
	// 	h.mirrorChangeLeverage(ctx, slaves, body)
	default:
		h.logger.Warn("Unknown mirror path",
			slog.String("path", path))
	}
}

// mirrorOrderCreate дублирует создание ордера на slave аккаунты
// Поддерживает открытие (side 1, 3) и закрытие (side 2, 4) позиций
func (h *Handler) mirrorOrderCreate(ctx context.Context, slaves []models.Account, body []byte) {
	// Парсим для логирования
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		h.logger.Error("Failed to parse order create request", slog.Any("error", err))
		return
	}

	symbol, _ := req["symbol"].(string)
	side, _ := req["side"].(float64)
	vol, _ := req["vol"].(float64)
	leverage, _ := req["leverage"].(float64)

	// Определяем тип операции
	orderType := "OPEN"
	if int(side) == 2 || int(side) == 4 {
		orderType = "CLOSE"
	}

	h.logger.Info("📊 Mirror order create",
		slog.String("type", orderType),
		slog.String("symbol", symbol),
		slog.Int("side", int(side)),
		slog.Int("vol", int(vol)),
		slog.Int("leverage", int(leverage)))

	var wg sync.WaitGroup
	for _, slave := range slaves {
		wg.Add(1)
		go func(acc models.Account) {
			defer wg.Done()

			client, err := mexc.NewClient(acc, h.logger)
			if err != nil {
				h.logger.Error("Failed to create MEXC client",
					slog.String("account", acc.Name),
					slog.Any("error", err))
				return
			}

			// Используем PlaceOrderRaw для точной репликации запроса
			orderID, err := client.PlaceOrderRaw(ctx, body)
			if err != nil {
				h.logger.Error("❌ Mirror order failed",
					slog.String("account", acc.Name),
					slog.String("type", orderType),
					slog.Any("error", err))
				return
			}

			h.logger.Info("✅ Mirror order success",
				slog.String("account", acc.Name),
				slog.String("type", orderType),
				slog.String("order_id", orderID))
		}(slave)
	}
	wg.Wait()
}

// mirrorPlanOrderPlace дублирует установку SL/TP на slave аккаунты
func (h *Handler) mirrorPlanOrderPlace(ctx context.Context, slaves []models.Account, body []byte) {
	// Парсим для логирования
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		h.logger.Error("Failed to parse plan order place request", slog.Any("error", err))
		return
	}

	symbol, _ := req["symbol"].(string)
	stopLossPrice, _ := req["stopLossPrice"].(float64)
	takeProfitPrice, _ := req["takeProfitPrice"].(float64)

	h.logger.Info("📊 Mirror plan order place",
		slog.String("symbol", symbol),
		slog.Float64("stop_loss", stopLossPrice),
		slog.Float64("take_profit", takeProfitPrice))

	var wg sync.WaitGroup
	for _, slave := range slaves {
		wg.Add(1)
		go func(acc models.Account) {
			defer wg.Done()

			client, err := mexc.NewClient(acc, h.logger)
			if err != nil {
				h.logger.Error("Failed to create MEXC client",
					slog.String("account", acc.Name),
					slog.Any("error", err))
				return
			}

			err = client.SetStopLossRaw(ctx, body)
			if err != nil {
				h.logger.Error("❌ Mirror set SL/TP failed",
					slog.String("account", acc.Name),
					slog.Any("error", err))
				return
			}

			h.logger.Info("✅ Mirror set SL/TP success",
				slog.String("account", acc.Name))
		}(slave)
	}
	wg.Wait()
}

// mirrorStopOrderCancel дублирует отмену stop order на slave аккаунты
func (h *Handler) mirrorStopOrderCancel(ctx context.Context, slaves []models.Account, body []byte) {
	h.logger.Info("📊 Mirror stop order cancel")

	var wg sync.WaitGroup
	for _, slave := range slaves {
		wg.Add(1)
		go func(acc models.Account) {
			defer wg.Done()

			client, err := mexc.NewClient(acc, h.logger)
			if err != nil {
				h.logger.Error("Failed to create MEXC client",
					slog.String("account", acc.Name),
					slog.Any("error", err))
				return
			}

			err = client.CancelStopLossRaw(ctx, body)
			if err != nil {
				h.logger.Error("❌ Mirror cancel stop order failed",
					slog.String("account", acc.Name),
					slog.Any("error", err))
			} else {
				h.logger.Info("✅ Mirror cancel stop order success",
					slog.String("account", acc.Name))
			}
		}(slave)
	}
	wg.Wait()
}

// mirrorChangePlanPrice дублирует изменение цены stop loss на slave аккаунты
func (h *Handler) mirrorChangePlanPrice(ctx context.Context, slaves []models.Account, body []byte) {
	// Парсим для логирования
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		h.logger.Error("Failed to parse change plan price request", slog.Any("error", err))
		return
	}

	stopLossPrice, _ := req["stopLossPrice"].(float64)

	h.logger.Info("📊 Mirror change plan price",
		slog.Float64("stop_loss_price", stopLossPrice))

	var wg sync.WaitGroup
	for _, slave := range slaves {
		wg.Add(1)
		go func(acc models.Account) {
			defer wg.Done()

			client, err := mexc.NewClient(acc, h.logger)
			if err != nil {
				h.logger.Error("Failed to create MEXC client",
					slog.String("account", acc.Name),
					slog.Any("error", err))
				return
			}

			err = client.ChangeStopLossRaw(ctx, body)
			if err != nil {
				h.logger.Error("❌ Mirror change stop loss failed",
					slog.String("account", acc.Name),
					slog.Any("error", err))
				return
			}

			h.logger.Info("✅ Mirror change stop loss success",
				slog.String("account", acc.Name))
		}(slave)
	}
	wg.Wait()
}

// mirrorChangeLeverage дублирует изменение leverage на slave аккаунты
func (h *Handler) mirrorChangeLeverage(ctx context.Context, slaves []models.Account, body []byte) {
	// Парсим для логирования
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		h.logger.Error("Failed to parse change leverage request", slog.Any("error", err))
		return
	}

	symbol, _ := req["symbol"].(string)
	leverage, _ := req["leverage"].(float64)
	positionType, _ := req["positionType"].(float64)

	h.logger.Info("📊 Mirror change leverage",
		slog.String("symbol", symbol),
		slog.Int("leverage", int(leverage)),
		slog.Int("position_type", int(positionType)))

	var wg sync.WaitGroup
	for _, slave := range slaves {
		wg.Add(1)
		go func(acc models.Account) {
			defer wg.Done()

			client, err := mexc.NewClient(acc, h.logger)
			if err != nil {
				h.logger.Error("Failed to create MEXC client",
					slog.String("account", acc.Name),
					slog.Any("error", err))
				return
			}

			err = client.ChangeLeverageRaw(ctx, body)
			if err != nil {
				h.logger.Error("❌ Mirror change leverage failed",
					slog.String("account", acc.Name),
					slog.Any("error", err))
				return
			}

			h.logger.Info("✅ Mirror change leverage success",
				slog.String("account", acc.Name))
		}(slave)
	}
	wg.Wait()
}

// HandleGetMirrorScript возвращает JS код с токеном пользователя
func (h *Handler) HandleGetMirrorScript(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getUserFromContext(r)
	username, _ := h.getUsernameFromContext(r)

	// Генерируем токен для пользователя
	token := h.mirrorManager.GenerateToken(userID, username)

	script := GenerateMirrorScript(h.apiURL, token)

	h.respondSuccess(w, "", map[string]string{
		"script":     script,
		"token":      token,
		"mirror_url": h.apiURL,
	})
}

func (h *Handler) getUserFromContext(r *http.Request) (int, bool) {
	return middleware.GetUserID(r.Context())
}

func (h *Handler) getUsernameFromContext(r *http.Request) (string, bool) {
	return middleware.GetUsername(r.Context())
}

// GenerateMirrorScript генерирует JS скрипт для browser mirror
func GenerateMirrorScript(mirrorURL, token string) string {
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

// MirrorStatus - статус mirror mode
type MirrorStatus struct {
	Active    bool   `json:"active"`
	Token     string `json:"token,omitempty"`
	MirrorURL string `json:"mirror_url,omitempty"`
}

// HandleStartMirror активирует Browser Mirror mode
func (h *Handler) HandleStartMirror(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getUserFromContext(r)
	username, _ := h.getUsernameFromContext(r)

	// Останавливаем WebSocket copy trading если активен
	if h.copyTradingWeb.IsActive(userID) {
		if err := h.copyTradingWeb.Stop(userID); err != nil {
			h.logger.Warn("Failed to stop WebSocket copy trading",
				slog.Int("user_id", userID),
				slog.Any("error", err))
		} else {
			h.logger.Info("WebSocket copy trading stopped (switching to mirror mode)",
				slog.Int("user_id", userID))
		}
	}

	// Получаем или генерируем токен
	token := h.mirrorManager.GetTokenForUser(userID, username)

	// Активируем mirror mode
	h.mirrorManager.SetActive(userID, true)

	h.respondSuccess(w, "Mirror mode started", map[string]any{
		"token":      token,
		"mirror_url": h.apiURL,
	})
}

// HandleStopMirror деактивирует Browser Mirror mode
func (h *Handler) HandleStopMirror(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getUserFromContext(r)

	h.mirrorManager.SetActive(userID, false)

	h.respondSuccess(w, "Mirror mode stopped", nil)
}

// HandleGetMirrorStatus возвращает статус mirror mode
func (h *Handler) HandleGetMirrorStatus(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getUserFromContext(r)
	username, _ := h.getUsernameFromContext(r)

	active := h.mirrorManager.IsActive(userID)

	status := MirrorStatus{
		Active: active,
	}

	if active {
		status.Token = h.mirrorManager.GetTokenForUser(userID, username)
		status.MirrorURL = h.apiURL
	}

	h.respondSuccess(w, "", status)
}
