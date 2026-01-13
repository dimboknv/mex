package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"tg_mexc/internal/middleware"
	"tg_mexc/pkg/services/copytrading"
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
	// Обрабатываем запрос в зависимости от пути
	session, err := h.manager.GetSession(mirrorToken.UserID, "mirror")
	if err != nil {
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
	go h.processMirrorRequest(session, path, body)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success":true}`))
}

// processMirrorRequest обрабатывает mirror запрос и выполняет его на slave аккаунтах
func (h *Handler) processMirrorRequest(session *copytrading.Session, path string, body []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch {
	case strings.HasSuffix(path, "/order/create"):
		return h.mirrorOrderCreate(ctx, session, body)
	case strings.HasSuffix(path, "/planorder/place"):
		return h.mirrorPlanOrderPlace(ctx, session, body)
	case strings.HasSuffix(path, "/stoporder/cancel"):
		return h.mirrorStopOrderCancel(ctx, session, body)
	case strings.HasSuffix(path, "/stoporder/change_plan_price"):
		return h.mirrorChangePlanPrice(ctx, session, body)
	case strings.HasSuffix(path, "/change_leverage"):
		return h.mirrorChangeLeverage(ctx, session, body)
	default:
		return fmt.Errorf("unknown mirror path: %s", path)
	}
}

// mirrorOrderCreate дублирует создание ордера на slave аккаунты
// Поддерживает открытие (side 1, 3) и закрытие (side 2, 4) позиций
func (h *Handler) mirrorOrderCreate(ctx context.Context, session *copytrading.Session, body []byte) error {
	openReq, closeReq, err := parseMirrorOrderCreate(body)
	if err != nil {
		return fmt.Errorf("failed to parse order create request: %w", err)
	}

	var result copytrading.ExecutionResult
	var orderType string

	if openReq != nil {
		// todo: logs:
		result, err = session.OpenPosition(ctx, *openReq)
	} else {
		// todo: logs:
		result, err = session.ClosePosition(ctx, *closeReq)
	}

	if err != nil {
		return fmt.Errorf("failed to execute mirror order create: %w", err)
	}

	// Логируем результаты
	for _, r := range result.Results {
		if r.Success {
			h.logger.Info("✅ Mirror order success",
				slog.String("account", r.AccountName),
				slog.String("type", orderType),
				slog.String("order_id", r.OrderID))
		} else {
			h.logger.Error("❌ Mirror order failed",
				slog.String("account", r.AccountName),
				slog.String("type", orderType),
				slog.String("error", r.Error))
		}
	}

	return nil
}

// mirrorPlanOrderPlace дублирует установку SL/TP на slave аккаунты
func (h *Handler) mirrorPlanOrderPlace(ctx context.Context, session *copytrading.Session, body []byte) error {
	req, err := parseMirrorStopLoss(body)
	if err != nil {
		return fmt.Errorf("failed to parse plan order place request: %w", err)
	}

	h.logger.Info("📊 Mirror plan order place",
		slog.String("symbol", req.Symbol),
		slog.Float64("stop_loss", req.StopLossPrice),
		slog.Float64("take_profit", req.TakeProfitPrice))

	result, err := session.PlacePlanOrder(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to execute mirror plan order place: %w", err)
	}

	// Логируем результаты
	for _, r := range result.Results {
		if r.Success {
			h.logger.Info("✅ Mirror set SL/TP success",
				slog.String("account", r.AccountName))
		} else {
			h.logger.Error("❌ Mirror set SL/TP failed",
				slog.String("account", r.AccountName),
				slog.String("error", r.Error))
		}
	}

	return nil
}

// mirrorStopOrderCancel дублирует отмену stop order на slave аккаунты
func (h *Handler) mirrorStopOrderCancel(ctx context.Context, session *copytrading.Session, body []byte) error {
	orderIDs, err := parseMirrorCancelStopOrder(body)
	if err != nil {
		return fmt.Errorf("failed to parse cancel stop order request: %w", err)
	}

	if _, err := session.CancelStopOrder(ctx, orderIDs); err != nil {
		return fmt.Errorf("failed to execute mirror stop order cancel: %w", err)
	}

	return nil

}

// mirrorChangePlanPrice дублирует изменение цены stop loss на slave аккаунты
func (h *Handler) mirrorChangePlanPrice(ctx context.Context, session *copytrading.Session, body []byte) error {
	req, err := parseChangePlanPrice(body)
	if err != nil {
		return fmt.Errorf("failed to parse change plan price request")
	}

	if _, err := session.ChangePlanPrice(ctx, req); err != nil {
		return fmt.Errorf("failed to execute mirror change plan price: %w", err)
	}

	return nil
}

// mirrorChangeLeverage дублирует изменение leverage на slave аккаунты
func (h *Handler) mirrorChangeLeverage(ctx context.Context, session *copytrading.Session, body []byte) error {
	req, err := parseMirrorChangeLeverage(body)
	if err != nil {
		return fmt.Errorf("failed to parse change leverage request: %w", err)
	}

	result, err := session.ChangeLeverage(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to execute mirror change leverage: %w", err)
	}

	for _, r := range result.Results {
		if r.Success {
			h.logger.Info("✅ Mirror change leverage success",
				slog.String("account", r.AccountName))
		} else {
			h.logger.Error("❌ Mirror change leverage failed",
				slog.String("account", r.AccountName),
				slog.String("error", r.Error))
		}
	}

	return nil
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

func (h *Handler) HandleStartMirror(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getUserFromContext(r)
	username, _ := h.getUsernameFromContext(r)

	if _, err := h.manager.CreateOrGetActiveSession(userID, "mirror"); err != nil {
		h.respondError(w, http.StatusConflict, err.Error())
		return
	}

	// Получаем или генерируем токен
	token := h.mirrorManager.GetTokenForUser(userID, username)

	h.respondSuccess(w, "Mirror mode started", map[string]any{
		"token":      token,
		"mirror_url": h.apiURL,
	})
}

func (h *Handler) HandleStopMirror(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getUserFromContext(r)

	// Останавливаем WebSocket copy trading если активен
	if err := h.manager.StopSession(userID, "mirror"); err != nil {
		h.respondError(w, http.StatusConflict, err.Error())
		return
	}

	h.respondSuccess(w, "Mirror mode stopped", nil)
}

func (h *Handler) HandleGetMirrorStatus(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getUserFromContext(r)
	username, _ := h.getUsernameFromContext(r)

	status := MirrorStatus{}

	if _, err := h.manager.GetSession(userID, "mirror"); err == nil {
		status.Active = true
		status.Token = h.mirrorManager.GetTokenForUser(userID, username)
		status.MirrorURL = h.apiURL
	}

	h.respondSuccess(w, "", status)
}

type mirrorOrderCreate struct {
	Symbol        string  `json:"symbol"`
	Side          int     `json:"side"`
	Vol           int     `json:"vol"`
	Leverage      int     `json:"leverage"`
	OpenType      int     `json:"openType"`
	Type          any     `json:"type"` // может быть string или int
	StopLossPrice string  `json:"stopLossPrice,omitempty"`
	LossTrend     string  `json:"lossTrend,omitempty"`
	PositionID    int64   `json:"positionId,omitempty"`
	Price         float64 `json:"price,omitempty"`
}

func parseMirrorOrderCreate(body []byte) (*copytrading.OpenPositionRequest, *copytrading.ClosePositionRequest, error) {
	var raw mirrorOrderCreate
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, fmt.Errorf("failed to parse order create: %w", err)
	}

	switch raw.Side {
	case 1, 3: // open long, open short
		var stopLoss float64
		if raw.StopLossPrice != "" {
			fmt.Sscanf(raw.StopLossPrice, "%f", &stopLoss)
		}
		return &copytrading.OpenPositionRequest{
			Symbol:        raw.Symbol,
			Side:          raw.Side,
			Volume:        float64(raw.Vol),
			Leverage:      raw.Leverage,
			StopLossPrice: stopLoss,
		}, nil, nil
	case 2, 4: // close short, close long
		return nil, &copytrading.ClosePositionRequest{Symbol: raw.Symbol}, nil
	default:
		return nil, nil, fmt.Errorf("unknown side: %d", raw.Side)
	}
}

type mirrorPlanOrderPlace struct {
	Symbol          string  `json:"symbol"`
	StopLossPrice   float64 `json:"stopLossPrice"`
	TakeProfitPrice float64 `json:"takeProfitPrice"`
	LossTrend       int     `json:"lossTrend"`
	ProfitTrend     int     `json:"profitTrend"`
}

func parseMirrorStopLoss(body []byte) (copytrading.PlacePlanOrderRequest, error) {
	var raw mirrorPlanOrderPlace
	if err := json.Unmarshal(body, &raw); err != nil {
		return copytrading.PlacePlanOrderRequest{}, fmt.Errorf("failed to parse plan order place: %w", err)
	}

	return copytrading.PlacePlanOrderRequest{
		Symbol:          raw.Symbol,
		StopLossPrice:   raw.StopLossPrice,
		TakeProfitPrice: raw.TakeProfitPrice,
		LossTrend:       raw.LossTrend,
		ProfitTrend:     raw.ProfitTrend,
	}, nil
}

type mirrorStopOrderCancel struct {
	StopPlanOrderID int `json:"stopPlanOrderId"`
}

func parseMirrorCancelStopOrder(body []byte) ([]int, error) {
	var raw []mirrorStopOrderCancel
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse cancel stop order: %w", err)
	}

	res := make([]int, 0, len(raw))
	for _, r := range raw {
		res = append(res, r.StopPlanOrderID)
	}

	return res, nil
}

type mirrorChangePlanPrice struct {
	StopPlanOrderID   int     `json:"stopPlanOrderId"`
	StopLossPrice     float64 `json:"stopLossPrice"`
	LossTrend         int     `json:"lossTrend"`
	ProfitTrend       int     `json:"profitTrend"`
	StopLossReverse   int     `json:"stopLossReverse"`
	TakeProfitReverse int     `json:"takeProfitReverse"`
}

func parseChangePlanPrice(body []byte) (copytrading.ChangePlanPriceRequest, error) {
	var raw mirrorChangePlanPrice
	if err := json.Unmarshal(body, &raw); err != nil {
		return copytrading.ChangePlanPriceRequest{}, fmt.Errorf("failed to parse change plan price: %w", err)
	}
	return copytrading.ChangePlanPriceRequest{
		StopPlanOrderID:   raw.StopPlanOrderID,
		StopLossPrice:     raw.StopLossPrice,
		LossTrend:         raw.LossTrend,
		ProfitTrend:       raw.ProfitTrend,
		StopLossReverse:   raw.StopLossReverse,
		TakeProfitReverse: raw.TakeProfitReverse,
	}, nil
}

type mirrorChangeLeverage struct {
	Symbol       string `json:"symbol"`
	Leverage     int    `json:"leverage"`
	OpenType     int    `json:"openType"`
	PositionType int    `json:"positionType"`
}

func parseMirrorChangeLeverage(body []byte) (copytrading.ChangeLeverageRequest, error) {
	var raw mirrorChangeLeverage
	if err := json.Unmarshal(body, &raw); err != nil {
		return copytrading.ChangeLeverageRequest{}, fmt.Errorf("failed to parse change leverage: %w", err)
	}

	return copytrading.ChangeLeverageRequest{
		Symbol:       raw.Symbol,
		Leverage:     raw.Leverage,
		OpenType:     raw.OpenType,
		PositionType: raw.PositionType,
	}, nil
}
