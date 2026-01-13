package wscopytrading

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	copytrading "tg_mexc/internal/mexc/copytrading"
	"tg_mexc/internal/mexc/websocket"
)

// Service - сервис copy trading для Web App
type Service struct {
	wsClient *websocket.Client
	logger   *slog.Logger
	session  *copytrading.Session
}

// NewService создает новый сервис copy trading для Web App
func NewService(session *copytrading.Session, logger *slog.Logger) *Service {
	return &Service{
		logger:  logger,
		session: session,
	}
}

func (s *Service) Start() error {
	masterAccount, err := s.session.GetMasterAccount()
	if err != nil {
		return fmt.Errorf("failed to get master account: %w", err)
	}

	wsClient := websocket.New(masterAccount, s.logger)

	timeoutCtx := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 5*time.Second)
	}

	wsClient.SetOrderHandler(func(event any) {
		if order, ok := event.(websocket.OrderEvent); ok {
			ctx, cancel := timeoutCtx()
			defer cancel()
			s.handleOrderEvent(ctx, order)
		}
	})

	wsClient.SetStopOrderHandler(func(event any) {
		if stop, ok := event.(websocket.StopOrderEvent); ok {
			ctx, cancel := timeoutCtx()
			defer cancel()
			s.handleStopOrderEvent(ctx, stop)
		}
	})

	wsClient.SetStopPlanOrderHandler(func(event any) {
		if stopPlan, ok := event.(websocket.StopPlanOrderEvent); ok {
			ctx, cancel := timeoutCtx()
			defer cancel()
			s.handleStopPlanOrderEvent(ctx, stopPlan)
		}
	})

	wsClient.SetPositionHandler(func(event any) {
		if pos, ok := event.(websocket.PositionEvent); ok {
			ctx, cancel := timeoutCtx()
			defer cancel()
			s.handlePositionEvent(ctx, pos)
		}
	})

	if err := wsClient.Connect(); err != nil {
		return fmt.Errorf("websocket connection error: %w", err)
	}

	s.wsClient = wsClient

	return nil
}

func (s *Service) Stop() error {
	return s.wsClient.Disconnect()
}

// handleOrderEvent обрабатывает событие ордера для Service
func (s *Service) handleOrderEvent(ctx context.Context, order websocket.OrderEvent) {
	openReq, closeReq := fromWebSocketOrder(order)
	if openReq == nil && closeReq == nil {
		s.logger.Debug("Unknown order side", slog.Int("side", order.Side))
		return
	}

	var err error
	if copytrading.IsOpenOrder(order.Side) {
		_, err = s.session.OpenPosition(ctx, *openReq)
	} else {
		_, err = s.session.ClosePosition(ctx, *closeReq)
	}

	if err != nil {
		s.logger.Error("Failed to execute order create", slog.Any("error", err))
	}
}

// handleStopOrderEvent обрабатывает событие stop order для Service
func (s *Service) handleStopOrderEvent(ctx context.Context, stop websocket.StopOrderEvent) {
	// Кэшируем stop order для оптимизации последующих lookup'ов
	if stop.OrderID != "" && stop.Symbol != "" {
		if err := s.session.SaveStopOrder(stop.OrderID, stop.Symbol); err != nil {
			s.logger.Warn("Failed to cache stop order", slog.Any("error", err))
		}
	}

	req := fromWebSocketStopOrder(stop)
	if _, err := s.session.PlacePlanOrder(ctx, req); err != nil {
		s.logger.Error("Failed to execute place plan order", slog.Any("error", err))
	}
}

// handleStopPlanOrderEvent обрабатывает событие изменения/отмены SL/TP для Service
func (s *Service) handleStopPlanOrderEvent(ctx context.Context, stopPlan websocket.StopPlanOrderEvent) {
	// Кэшируем stop order для оптимизации последующих lookup'ов
	if stopPlan.OrderId != "" && stopPlan.Symbol != "" {
		if err := s.session.SaveStopOrder(stopPlan.OrderId, stopPlan.Symbol); err != nil {
			s.logger.Warn("Failed to cache stop order", slog.Any("error", err))
		}
	}

	// isFinished == 1 означает отмену стоп-ордера
	if stopPlan.IsFinished == 1 {
		if _, err := s.session.CancelStopOrderBySymbol(ctx, stopPlan.Symbol); err != nil {
			s.logger.Error("Failed to cancel stop order", slog.Any("error", err))
		}
		return
	}

	// isFinished == 0 означает изменение стоп-ордера
	req := fromWebSocketStopPlanOrder(stopPlan)
	if _, err := s.session.ChangePlanPrice(ctx, req); err != nil {
		s.logger.Error("Failed to change plan price", slog.Any("error", err))
	}
}

// handlePositionEvent обрабатывает событие позиции для Service
func (s *Service) handlePositionEvent(ctx context.Context, pos websocket.PositionEvent) {
	closeReq := fromWebSocketPosition(pos)
	if closeReq == nil {
		return
	}

	if _, err := s.session.ClosePosition(ctx, *closeReq); err != nil {
		s.logger.Error("Failed to execute close position", slog.Any("error", err))
	}
}

// fromWebSocketOrder конвертирует websocket.OrderEvent в запрос
// Возвращает либо OpenPositionRequest, либо ClosePositionRequest
func fromWebSocketOrder(event websocket.OrderEvent) (openReq *copytrading.OpenPositionRequest, closeReq *copytrading.ClosePositionRequest) {
	switch event.Side {
	case 1, 3: // open long, open short
		var stopLoss float64
		if event.StopOrderEvent != nil && event.StopOrderEvent.StopLossPrice > 0 {
			stopLoss = event.StopOrderEvent.StopLossPrice
		}
		return &copytrading.OpenPositionRequest{
			Symbol:        event.Symbol,
			Side:          event.Side,
			Volume:        event.Vol,
			Leverage:      event.Leverage,
			StopLossPrice: stopLoss,
		}, nil
	case 2, 4: // close short, close long
		return nil, &copytrading.ClosePositionRequest{
			Symbol: event.Symbol,
			Side:   event.Side,
			Volume: event.Vol,
		}
	}
	return nil, nil
}

// fromWebSocketStopOrder конвертирует websocket.StopOrderEvent в PlacePlanOrderRequest
func fromWebSocketStopOrder(event websocket.StopOrderEvent) copytrading.PlacePlanOrderRequest {
	return copytrading.PlacePlanOrderRequest{
		Symbol:          event.Symbol,
		StopLossPrice:   event.StopLossPrice,
		TakeProfitPrice: event.TakeProfitPrice,
		LossTrend:       event.LossTrend,
		ProfitTrend:     event.ProfitTrend,
	}
}

// fromWebSocketStopPlanOrder конвертирует websocket.StopPlanOrderEvent в ChangePlanPriceRequest
func fromWebSocketStopPlanOrder(event websocket.StopPlanOrderEvent) copytrading.ChangePlanPriceRequest {
	orderID, _ := strconv.Atoi(event.OrderId)
	return copytrading.ChangePlanPriceRequest{
		StopPlanOrderID:   orderID,
		Symbol:            event.Symbol, // Передаём symbol напрямую - не нужен API lookup
		StopLossPrice:     event.StopLossPrice,
		LossTrend:         event.LossTrend,
		ProfitTrend:       event.ProfitTrend,
		StopLossReverse:   event.StopLossReverse,
		TakeProfitReverse: event.TakeProfitReverse,
	}
}

// fromWebSocketPosition конвертирует websocket.PositionEvent в ClosePositionRequest
// Возвращает nil если позиция не закрыта (state != 3)
func fromWebSocketPosition(event websocket.PositionEvent) *copytrading.ClosePositionRequest {
	if event.State != 3 { // только закрытие позиций
		return nil
	}
	return &copytrading.ClosePositionRequest{Symbol: event.Symbol}
}
