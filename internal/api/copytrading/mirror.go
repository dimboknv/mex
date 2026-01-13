package copytrading

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	copytrading2 "tg_mexc/internal/mexc/copytrading"
)

// mirrorToken - токен для идентификации пользователя
type mirrorToken struct {
	Token     string
	UserID    int
	Username  string
	CreatedAt time.Time
}

// mirrorService реализует MirrorService
type mirrorService struct {
	manager *copytrading2.Manager
	storage AccountStorage
	logger  *slog.Logger
	apiURL  string
	tokens  map[string]*mirrorToken
	active  map[int]bool
	mu      sync.RWMutex
}

// NewMirrorService создаёт новый Mirror сервис
func NewMirrorService(
	manager *copytrading2.Manager,
	storage AccountStorage,
	apiURL string,
	logger *slog.Logger,
) MirrorService {
	return &mirrorService{
		manager: manager,
		storage: storage,
		apiURL:  apiURL,
		logger:  logger,
		tokens:  make(map[string]*mirrorToken),
		active:  make(map[int]bool),
	}
}

func (s *mirrorService) Start(ctx context.Context, userID int, username string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Проверяем что есть master аккаунт
	if _, err := s.storage.GetMasterAccount(userID); err != nil {
		return "", fmt.Errorf("master account not set: %w", err)
	}

	// Создаём сессию в manager
	if _, err := s.manager.CreateOrGetActiveSession(userID, "mirror"); err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	// Генерируем или получаем токен
	token := s.getOrCreateTokenLocked(userID, username)
	s.active[userID] = true

	s.logger.Info("Mirror copy trading started",
		slog.Int("user_id", userID),
		slog.String("username", username))

	return token, nil
}

func (s *mirrorService) Stop(ctx context.Context, userID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.manager.StopSession(userID, "mirror"); err != nil {
		return fmt.Errorf("failed to stop session: %w", err)
	}

	delete(s.active, userID)

	s.logger.Info("Mirror copy trading stopped", slog.Int("user_id", userID))

	return nil
}

func (s *mirrorService) IsActive(userID int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active[userID]
}

func (s *mirrorService) GetToken(userID int, username string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getOrCreateTokenLocked(userID, username)
}

func (s *mirrorService) ProcessRequest(ctx context.Context, token string, path string, body []byte) error {
	userID, _, ok := s.ValidateToken(token)
	if !ok {
		return fmt.Errorf("invalid token")
	}

	session, err := s.manager.GetSession(userID, "mirror")
	if err != nil {
		s.logger.Debug("Mirror request ignored - not active", slog.Int("user_id", userID))
		return nil
	}

	return s.processRequest(ctx, session, path, body)
}

func (s *mirrorService) ValidateToken(token string) (userID int, username string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mt, exists := s.tokens[token]
	if !exists {
		return 0, "", false
	}

	return mt.UserID, mt.Username, true
}

// getOrCreateTokenLocked - вызывать только с удержанным lock
func (s *mirrorService) getOrCreateTokenLocked(userID int, username string) string {
	// Ищем существующий токен
	for _, mt := range s.tokens {
		if mt.UserID == userID {
			return mt.Token
		}
	}

	// Удаляем старый токен если есть
	for token, mt := range s.tokens {
		if mt.UserID == userID {
			delete(s.tokens, token)
			break
		}
	}

	// Генерируем новый
	bytes := make([]byte, 16)
	rand.Read(bytes)
	token := hex.EncodeToString(bytes)

	s.tokens[token] = &mirrorToken{
		Token:     token,
		UserID:    userID,
		Username:  username,
		CreatedAt: time.Now(),
	}

	return token
}

func (s *mirrorService) getAPIURL() string {
	return s.apiURL
}

func (s *mirrorService) stopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for userID := range s.active {
		_ = s.manager.StopSession(userID, "mirror")
		s.logger.Info("Mirror stopped (shutdown)", slog.Int("user_id", userID))
	}

	s.active = make(map[int]bool)
}

// processRequest обрабатывает mirror запрос
func (s *mirrorService) processRequest(ctx context.Context, session *copytrading2.Session, path string, body []byte) error {
	switch {
	case strings.HasSuffix(path, "/order/create"):
		return s.handleOrderCreate(ctx, session, body)
	case strings.HasSuffix(path, "/planorder/place"):
		return s.handlePlanOrderPlace(ctx, session, body)
	case strings.HasSuffix(path, "/stoporder/cancel"):
		return s.handleStopOrderCancel(ctx, session, body)
	case strings.HasSuffix(path, "/stoporder/change_plan_price"):
		return s.handleChangePlanPrice(ctx, session, body)
	case strings.HasSuffix(path, "/change_leverage"):
		return s.handleChangeLeverage(ctx, session, body)
	default:
		return fmt.Errorf("unknown mirror path: %s", path)
	}
}

// === Request handlers ===

func (s *mirrorService) handleOrderCreate(ctx context.Context, session *copytrading2.Session, body []byte) error {
	openReq, closeReq, err := s.parseOrderCreate(body)
	if err != nil {
		return fmt.Errorf("failed to parse order create: %w", err)
	}

	if openReq != nil {
		result, err := session.OpenPosition(ctx, *openReq)
		if err != nil {
			return fmt.Errorf("failed to open position: %w", err)
		}
		s.logResult("open position", result)
	} else {
		result, err := session.ClosePosition(ctx, *closeReq)
		if err != nil {
			return fmt.Errorf("failed to close position: %w", err)
		}
		s.logResult("close position", result)
	}

	return nil
}

func (s *mirrorService) handlePlanOrderPlace(ctx context.Context, session *copytrading2.Session, body []byte) error {
	req, err := s.parsePlanOrderPlace(body)
	if err != nil {
		return fmt.Errorf("failed to parse plan order: %w", err)
	}

	result, err := session.PlacePlanOrder(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to place plan order: %w", err)
	}
	s.logResult("place plan order", result)

	return nil
}

func (s *mirrorService) handleStopOrderCancel(ctx context.Context, session *copytrading2.Session, body []byte) error {
	orderIDs, err := s.parseStopOrderCancel(body)
	if err != nil {
		return fmt.Errorf("failed to parse stop order cancel: %w", err)
	}

	if _, err := session.CancelStopOrder(ctx, orderIDs); err != nil {
		return fmt.Errorf("failed to cancel stop order: %w", err)
	}

	return nil
}

func (s *mirrorService) handleChangePlanPrice(ctx context.Context, session *copytrading2.Session, body []byte) error {
	req, err := s.parseChangePlanPrice(body)
	if err != nil {
		return fmt.Errorf("failed to parse change plan price: %w", err)
	}

	if _, err := session.ChangePlanPrice(ctx, req); err != nil {
		return fmt.Errorf("failed to change plan price: %w", err)
	}

	return nil
}

func (s *mirrorService) handleChangeLeverage(ctx context.Context, session *copytrading2.Session, body []byte) error {
	req, err := s.parseChangeLeverage(body)
	if err != nil {
		return fmt.Errorf("failed to parse change leverage: %w", err)
	}

	result, err := session.ChangeLeverage(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to change leverage: %w", err)
	}
	s.logResult("change leverage", result)

	return nil
}

// === Parsers ===

type orderCreateRequest struct {
	Symbol        string `json:"symbol"`
	Side          int    `json:"side"`
	Vol           int    `json:"vol"`
	Leverage      int    `json:"leverage"`
	StopLossPrice string `json:"stopLossPrice,omitempty"`
	PositionID    int64  `json:"positionId,omitempty"`
}

func (s *mirrorService) parseOrderCreate(body []byte) (*copytrading2.OpenPositionRequest, *copytrading2.ClosePositionRequest, error) {
	var raw orderCreateRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, err
	}

	switch raw.Side {
	case 1, 3:
		var stopLoss float64
		if raw.StopLossPrice != "" {
			fmt.Sscanf(raw.StopLossPrice, "%f", &stopLoss)
		}
		return &copytrading2.OpenPositionRequest{
			Symbol:        raw.Symbol,
			Side:          raw.Side,
			Volume:        float64(raw.Vol),
			Leverage:      raw.Leverage,
			StopLossPrice: stopLoss,
		}, nil, nil
	case 2, 4:
		return nil, &copytrading2.ClosePositionRequest{
			Symbol:     raw.Symbol,
			Side:       raw.Side,
			Volume:     float64(raw.Vol),
			PositionID: raw.PositionID,
		}, nil
	default:
		return nil, nil, fmt.Errorf("unknown side: %d", raw.Side)
	}
}

type planOrderPlaceRequest struct {
	Symbol          string  `json:"symbol"`
	StopLossPrice   float64 `json:"stopLossPrice"`
	TakeProfitPrice float64 `json:"takeProfitPrice"`
	LossTrend       int     `json:"lossTrend"`
	ProfitTrend     int     `json:"profitTrend"`
}

func (s *mirrorService) parsePlanOrderPlace(body []byte) (copytrading2.PlacePlanOrderRequest, error) {
	var raw planOrderPlaceRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return copytrading2.PlacePlanOrderRequest{}, err
	}

	return copytrading2.PlacePlanOrderRequest{
		Symbol:          raw.Symbol,
		StopLossPrice:   raw.StopLossPrice,
		TakeProfitPrice: raw.TakeProfitPrice,
		LossTrend:       raw.LossTrend,
		ProfitTrend:     raw.ProfitTrend,
	}, nil
}

type stopOrderCancelRequest struct {
	StopPlanOrderID int `json:"stopPlanOrderId"`
}

func (s *mirrorService) parseStopOrderCancel(body []byte) ([]int, error) {
	var raw []stopOrderCancelRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	ids := make([]int, 0, len(raw))
	for _, r := range raw {
		ids = append(ids, r.StopPlanOrderID)
	}

	return ids, nil
}

type changePlanPriceRequest struct {
	StopPlanOrderID   int     `json:"stopPlanOrderId"`
	StopLossPrice     float64 `json:"stopLossPrice"`
	LossTrend         int     `json:"lossTrend"`
	ProfitTrend       int     `json:"profitTrend"`
	StopLossReverse   int     `json:"stopLossReverse"`
	TakeProfitReverse int     `json:"takeProfitReverse"`
}

func (s *mirrorService) parseChangePlanPrice(body []byte) (copytrading2.ChangePlanPriceRequest, error) {
	var raw changePlanPriceRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return copytrading2.ChangePlanPriceRequest{}, err
	}

	return copytrading2.ChangePlanPriceRequest{
		StopPlanOrderID:   raw.StopPlanOrderID,
		StopLossPrice:     raw.StopLossPrice,
		LossTrend:         raw.LossTrend,
		ProfitTrend:       raw.ProfitTrend,
		StopLossReverse:   raw.StopLossReverse,
		TakeProfitReverse: raw.TakeProfitReverse,
	}, nil
}

type changeLeverageRequest struct {
	Symbol       string `json:"symbol"`
	Leverage     int    `json:"leverage"`
	OpenType     int    `json:"openType"`
	PositionType int    `json:"positionType"`
}

func (s *mirrorService) parseChangeLeverage(body []byte) (copytrading2.ChangeLeverageRequest, error) {
	var raw changeLeverageRequest
	if err := json.Unmarshal(body, &raw); err != nil {
		return copytrading2.ChangeLeverageRequest{}, err
	}

	return copytrading2.ChangeLeverageRequest{
		Symbol:       raw.Symbol,
		Leverage:     raw.Leverage,
		OpenType:     raw.OpenType,
		PositionType: raw.PositionType,
	}, nil
}

func (s *mirrorService) logResult(operation string, result copytrading2.ExecutionResult) {
	for _, r := range result.Results {
		if r.Success {
			s.logger.Info("Mirror "+operation+" success",
				slog.String("account", r.AccountName),
				slog.String("order_id", r.OrderID))
		} else {
			s.logger.Error("Mirror "+operation+" failed",
				slog.String("account", r.AccountName),
				slog.String("error", r.Error))
		}
	}
}
