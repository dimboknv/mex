package copytrading

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"tg_mexc/pkg/models"
	"tg_mexc/pkg/services/mexc"
	"tg_mexc/pkg/services/websocket"
	"tg_mexc/pkg/storage"
	"time"
)

// WebService - сервис copy trading для Web App
type WebService struct {
	storage  *storage.WebStorage
	logger   *slog.Logger
	dryRun   bool
	mu       sync.RWMutex
	sessions map[int]*WebSession // userID -> Session
}

// WebSession - сессия copy trading для Web App
type WebSession struct {
	userID       int
	wsClient     *websocket.Client
	masterAcc    models.Account
	slaveAccs    []models.Account
	logger       *slog.Logger
	storage      *storage.WebStorage
	dryRun       bool
	ignoreFees   bool
	active       bool
	mu           sync.RWMutex
	eventChannel chan string
}

// webOrderResult - результат обработки ордера для slave аккаунта
type webOrderResult struct {
	success bool
	detail  string
	error   string
	orderID string
}

// NewWebService создает новый сервис copy trading для Web App
func NewWebService(storage *storage.WebStorage, logger *slog.Logger, dryRun bool) *WebService {
	return &WebService{
		storage:  storage,
		logger:   logger,
		dryRun:   dryRun,
		sessions: make(map[int]*WebSession),
	}
}

// Start запускает copy trading для пользователя
func (s *WebService) Start(userID int, ignoreFees bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session, exists := s.sessions[userID]; exists && session.active {
		return fmt.Errorf("copy trading already running")
	}

	masterAcc, err := s.storage.GetMasterAccount(userID)
	if err != nil {
		return fmt.Errorf("master account not set")
	}

	slaveAccs, err := s.storage.GetSlaveAccounts(userID, ignoreFees)
	if err != nil {
		return fmt.Errorf("failed to get slave accounts: %w", err)
	}

	if len(slaveAccs) == 0 {
		return fmt.Errorf("no active slave accounts")
	}

	session := &WebSession{
		userID:       userID,
		masterAcc:    *masterAcc,
		slaveAccs:    slaveAccs,
		logger:       s.logger,
		storage:      s.storage,
		dryRun:       s.dryRun,
		ignoreFees:   ignoreFees,
		active:       true,
		eventChannel: make(chan string, 100),
	}

	wsClient := websocket.New(*masterAcc, s.logger)

	wsClient.SetOrderHandler(func(event any) {
		if order, ok := event.(websocket.OrderEvent); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session.handleOrderEvent(ctx, order)
		}
	})

	wsClient.SetStopOrderHandler(func(event any) {
		if stop, ok := event.(websocket.StopOrderEvent); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session.handleStopOrderEvent(ctx, stop)
		}
	})

	wsClient.SetStopPlanOrderHandler(func(event any) {
		if stopPlan, ok := event.(websocket.StopPlanOrderEvent); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session.handleStopPlanOrderEvent(ctx, stopPlan)
		}
	})

	wsClient.SetPositionHandler(func(event any) {
		if pos, ok := event.(websocket.PositionEvent); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session.handlePositionEvent(ctx, pos)
		}
	})

	wsClient.SetOrderDealHandler(func(event any) {
		if deal, ok := event.(websocket.DealEvent); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session.handleOrderDealEvent(ctx, deal)
		}
	})

	if err := wsClient.Connect(); err != nil {
		return fmt.Errorf("websocket connection error: %w", err)
	}

	session.wsClient = wsClient
	s.sessions[userID] = session

	// Логируем в activity log
	s.storage.AddLog(&models.ActivityLog{
		UserID:  &userID,
		Level:   "info",
		Action:  "copy_trading_start",
		Message: fmt.Sprintf("Copy trading started. Master: %s, Slaves: %d", masterAcc.Name, len(slaveAccs)),
	})

	s.logger.Info("Copy trading started",
		slog.Int("user_id", userID),
		slog.String("master", masterAcc.Name),
		slog.Int("slaves", len(slaveAccs)),
		slog.Bool("dry_run", s.dryRun))

	return nil
}

// Stop останавливает copy trading для пользователя
func (s *WebService) Stop(userID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[userID]
	if !exists || !session.active {
		return fmt.Errorf("copy trading not running")
	}

	if session.wsClient != nil {
		session.wsClient.Disconnect()
	}

	session.active = false
	close(session.eventChannel)
	delete(s.sessions, userID)

	// Логируем в activity log
	s.storage.AddLog(&models.ActivityLog{
		UserID:  &userID,
		Level:   "info",
		Action:  "copy_trading_stop",
		Message: "Copy trading stopped",
	})

	s.logger.Info("Copy trading stopped", slog.Int("user_id", userID))

	return nil
}

// IsActive проверяет, активен ли copy trading для пользователя
func (s *WebService) IsActive(userID int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[userID]

	return exists && session.active
}

// GetStatus возвращает статус copy trading для пользователя
func (s *WebService) GetStatus(userID int) (masterName string, slaveCount int, ignoreFees bool, isDryRun bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[userID]
	if !exists || !session.active {
		return "", 0, false, s.dryRun
	}

	return session.masterAcc.Name, len(session.slaveAccs), session.ignoreFees, session.dryRun
}

// GetEventChannel возвращает канал событий для пользователя
func (s *WebService) GetEventChannel(userID int) <-chan string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[userID]
	if !exists {
		return nil
	}

	return session.eventChannel
}

// StopAll останавливает все активные сессии (для graceful shutdown)
func (s *WebService) StopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for userID, session := range s.sessions {
		if session.wsClient != nil {
			session.wsClient.Disconnect()
		}
		session.active = false
		close(session.eventChannel)

		s.logger.Info("Copy trading stopped (shutdown)", slog.Int("user_id", userID))
	}

	s.sessions = make(map[int]*WebSession)
}

// IsDryRun возвращает статус dry run режима
func (s *WebService) IsDryRun() bool {
	return s.dryRun
}

// processSlaveOrderWeb обрабатывает ордер для slave аккаунта
func processSlaveOrderWeb(ctx context.Context, session *WebSession, slaveAcc models.Account, order websocket.OrderEvent, isOpenOrder bool, sideText string) webOrderResult {
	result := webOrderResult{success: false}

	// Проверяем disabled статус для операций открытия позиции
	if isOpenOrder && !session.ignoreFees && slaveAcc.Disabled {
		session.logger.Info("Skipping disabled account for open order",
			slog.String("slave", slaveAcc.Name))
		result.success = true
		result.detail = fmt.Sprintf("%s: skipped (disabled)", slaveAcc.Name)
		return result
	}

	client, err := mexc.NewClient(slaveAcc, session.logger)
	if err != nil {
		session.logger.Error("Failed to create client",
			slog.String("slave", slaveAcc.Name),
			slog.Any("error", err))
		result.error = err.Error()
		result.detail = fmt.Sprintf("%s: client error", slaveAcc.Name)
		return result
	}

	if isOpenOrder {
		currentLeverage, err := client.GetLeverageForSide(ctx, order.Symbol, order.Side)
		if err != nil {
			session.logger.Error("Failed to get leverage",
				slog.String("slave", slaveAcc.Name),
				slog.Any("error", err))
			result.error = err.Error()
			result.detail = fmt.Sprintf("%s: leverage error", slaveAcc.Name)
			return result
		}

		var stopLossPrice float64
		if order.StopOrderEvent != nil && order.StopOrderEvent.StopLossPrice > 0 {
			stopLossPrice = order.StopOrderEvent.StopLossPrice
		}

		if session.dryRun {
			session.logger.Info("DRY_RUN - Would place order",
				slog.String("slave", slaveAcc.Name),
				slog.String("symbol", order.Symbol),
				slog.Int("side", order.Side),
				slog.Float64("volume", order.Vol))
			result.success = true
			result.detail = fmt.Sprintf("%s: opened %s (DRY RUN)", slaveAcc.Name, sideText)
		} else {
			var orderID string
			if stopLossPrice > 0 {
				orderID, err = client.PlaceOrder(ctx, order.Symbol, order.Side, int(order.Vol), currentLeverage, stopLossPrice)
			} else {
				orderID, err = client.PlaceOrder(ctx, order.Symbol, order.Side, int(order.Vol), currentLeverage)
			}

			if err != nil {
				session.logger.Error("Failed to copy order",
					slog.String("slave", slaveAcc.Name),
					slog.Any("error", err))
				result.error = err.Error()
				result.detail = fmt.Sprintf("%s: order failed", slaveAcc.Name)
			} else {
				session.logger.Info("Order copied successfully",
					slog.String("slave", slaveAcc.Name),
					slog.String("order_id", orderID))
				result.success = true
				result.orderID = orderID
				result.detail = fmt.Sprintf("%s: opened %s", slaveAcc.Name, sideText)
			}
		}
	} else {
		if session.dryRun {
			session.logger.Info("DRY_RUN - Would close position",
				slog.String("slave", slaveAcc.Name),
				slog.String("symbol", order.Symbol))
			result.success = true
			result.detail = fmt.Sprintf("%s: closed %s (DRY RUN)", slaveAcc.Name, sideText)
		} else {
			err = client.ClosePosition(ctx, order.Symbol)
			if err != nil {
				session.logger.Error("Failed to close position",
					slog.String("slave", slaveAcc.Name),
					slog.Any("error", err))
				result.error = err.Error()
				result.detail = fmt.Sprintf("%s: close failed", slaveAcc.Name)
			} else {
				session.logger.Info("Position closed successfully",
					slog.String("slave", slaveAcc.Name))
				result.success = true
				result.detail = fmt.Sprintf("%s: closed %s", slaveAcc.Name, sideText)
			}
		}
	}

	time.Sleep(100 * time.Millisecond)
	return result
}

// processSlaveStopOrderWeb обрабатывает stop order для slave аккаунта
func processSlaveStopOrderWeb(ctx context.Context, session *WebSession, slaveAcc models.Account, stop websocket.StopOrderEvent) webOrderResult {
	result := webOrderResult{success: false}

	client, err := mexc.NewClient(slaveAcc, session.logger)
	if err != nil {
		session.logger.Error("Failed to create client",
			slog.String("slave", slaveAcc.Name),
			slog.Any("error", err))
		result.error = err.Error()
		return result
	}

	if session.dryRun {
		session.logger.Info("DRY_RUN - Would set SL/TP",
			slog.String("slave", slaveAcc.Name),
			slog.String("symbol", stop.Symbol))
		result.success = true
	} else {
		err = client.SetStopLoss(ctx, stop.Symbol, stop.StopLossPrice, stop.TakeProfitPrice)
		if err != nil {
			session.logger.Error("Failed to set SL/TP",
				slog.String("slave", slaveAcc.Name),
				slog.Any("error", err))
			result.error = err.Error()
		} else {
			session.logger.Info("SL/TP set successfully",
				slog.String("slave", slaveAcc.Name))
			result.success = true
		}
	}

	time.Sleep(100 * time.Millisecond)
	return result
}

// processSlaveStopPlanOrderWeb обрабатывает изменение SL/TP для slave аккаунта
func processSlaveStopPlanOrderWeb(ctx context.Context, session *WebSession, slaveAcc models.Account, stopPlan websocket.StopPlanOrderEvent, symbol string) webOrderResult {
	result := webOrderResult{success: false}

	client, err := mexc.NewClient(slaveAcc, session.logger)
	if err != nil {
		session.logger.Error("Failed to create client",
			slog.String("slave", slaveAcc.Name),
			slog.Any("error", err))
		result.error = err.Error()
		return result
	}

	slaveAccOrders, err := client.GetOpenStopOrders(ctx, symbol)
	if err != nil {
		session.logger.Error("Failed to get slave open orders",
			slog.String("slave", slaveAcc.Name),
			slog.Any("error", err))
		result.error = err.Error()
		return result
	}

	if len(slaveAccOrders) == 0 {
		session.logger.Debug("Order not found in slave's open orders",
			slog.String("slave", slaveAcc.Name))
		result.success = true // Не считаем ошибкой
		return result
	}

	slaveAccOrder := slaveAccOrders[0]

	if session.dryRun {
		session.logger.Info("DRY_RUN - Would update SL/TP",
			slog.String("slave", slaveAcc.Name),
			slog.String("symbol", symbol))
		result.success = true
	} else {
		r := models.ChangePlanPriceRequest{
			StopPlanOrderID:   slaveAccOrder.Id,
			LossTrend:         stopPlan.LossTrend,
			ProfitTrend:       stopPlan.ProfitTrend,
			StopLossReverse:   stopPlan.StopLossReverse,
			TakeProfitReverse: stopPlan.TakeProfitReverse,
			StopLossPrice:     stopPlan.StopLossPrice,
		}

		if err = client.ChangeStopLoss(ctx, r); err != nil {
			session.logger.Error("Failed to update SL/TP",
				slog.String("slave", slaveAcc.Name),
				slog.Any("error", err))
			result.error = err.Error()
		} else {
			session.logger.Info("SL/TP updated successfully",
				slog.String("slave", slaveAcc.Name))
			result.success = true
		}
	}

	time.Sleep(100 * time.Millisecond)
	return result
}

// processSlavePositionWeb закрывает позицию для slave аккаунта
func processSlavePositionWeb(ctx context.Context, session *WebSession, slaveAcc models.Account, pos websocket.PositionEvent) webOrderResult {
	result := webOrderResult{success: false}

	client, err := mexc.NewClient(slaveAcc, session.logger)
	if err != nil {
		session.logger.Error("Failed to create client",
			slog.String("slave", slaveAcc.Name),
			slog.Any("error", err))
		result.error = err.Error()
		return result
	}

	if session.dryRun {
		session.logger.Info("DRY_RUN - Would close position",
			slog.String("slave", slaveAcc.Name),
			slog.String("symbol", pos.Symbol))
		result.success = true
	} else {
		err = client.ClosePosition(ctx, pos.Symbol)
		if err != nil {
			session.logger.Error("Failed to close position",
				slog.String("slave", slaveAcc.Name),
				slog.Any("error", err))
			result.error = err.Error()
		} else {
			session.logger.Info("Position closed successfully",
				slog.String("slave", slaveAcc.Name))
			result.success = true
		}
	}

	time.Sleep(100 * time.Millisecond)
	return result
}

// handleOrderEvent обрабатывает событие ордера для WebSession
func (session *WebSession) handleOrderEvent(ctx context.Context, order websocket.OrderEvent) {
	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.active {
		return
	}

	session.logger.Info("Order event received",
		slog.String("master", session.masterAcc.Name),
		slog.Any("order", order),
	)

	var isOpenOrder bool
	var sideText string

	switch order.Side {
	case 1:
		isOpenOrder = true
		sideText = "LONG"
	case 2:
		isOpenOrder = false
		sideText = "SHORT"
	case 3:
		isOpenOrder = true
		sideText = "SHORT"
	case 4:
		isOpenOrder = false
		sideText = "LONG"
	default:
		session.logger.Debug("Unknown order side", slog.Int("side", order.Side))
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	failedCount := 0

	// Создаем запись trade
	masterAccID := session.masterAcc.ID
	trade := &models.Trade{
		UserID:          session.userID,
		MasterAccountID: &masterAccID,
		Symbol:          order.Symbol,
		Side:            order.Side,
		Volume:          int(order.Vol),
		Leverage:        order.Leverage,
		SentAt:          time.Now(),
		Status:          "processing",
	}
	tradeID, _ := session.storage.CreateTrade(trade)

	for _, slaveAcc := range session.slaveAccs {
		wg.Add(1)

		go func(acc models.Account) {
			defer wg.Done()

			startTime := time.Now()
			result := processSlaveOrderWeb(ctx, session, acc, order, isOpenOrder, sideText)
			latency := time.Since(startTime).Milliseconds()

			mu.Lock()
			if result.success {
				successCount++
			} else {
				failedCount++
			}

			// Сохраняем детали в БД
			if tradeID > 0 {
				status := "success"
				if !result.success {
					status = "failed"
				}
				session.storage.AddTradeDetail(&models.TradeDetail{
					TradeID:   tradeID,
					AccountID: acc.ID,
					Status:    status,
					Error:     result.error,
					OrderID:   result.orderID,
					LatencyMs: int(latency),
				})
			}
			mu.Unlock()
		}(slaveAcc)
	}

	wg.Wait()

	// Обновляем статус trade
	if tradeID > 0 {
		session.storage.UpdateTradeReceived(tradeID, time.Now())
		status := "completed"
		if failedCount > 0 && successCount == 0 {
			status = "failed"
		} else if failedCount > 0 {
			status = "partial"
		}
		session.storage.UpdateTradeStatus(tradeID, status, "")
	}

	// Логируем результат
	userID := session.userID
	session.storage.AddLog(&models.ActivityLog{
		UserID:  &userID,
		Level:   "info",
		Action:  "order_copied",
		Message: fmt.Sprintf("%s %s: %d/%d successful", order.Symbol, sideText, successCount, len(session.slaveAccs)),
	})
}

// handleStopOrderEvent обрабатывает событие stop order для WebSession
func (session *WebSession) handleStopOrderEvent(ctx context.Context, stop websocket.StopOrderEvent) {
	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.active {
		return
	}

	session.logger.Info("Stop order event received",
		slog.String("master", session.masterAcc.Name),
		slog.Any("event", stop),
	)

	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	failedCount := 0

	for _, slaveAcc := range session.slaveAccs {
		wg.Add(1)

		go func(acc models.Account) {
			defer wg.Done()

			result := processSlaveStopOrderWeb(ctx, session, acc, stop)

			mu.Lock()
			if result.success {
				successCount++
			} else {
				failedCount++
			}
			mu.Unlock()
		}(slaveAcc)
	}

	wg.Wait()

	userID := session.userID
	session.storage.AddLog(&models.ActivityLog{
		UserID:  &userID,
		Level:   "info",
		Action:  "sl_tp_set",
		Message: fmt.Sprintf("%s SL/TP set: %d/%d successful", stop.Symbol, successCount, len(session.slaveAccs)),
	})
}

// handleStopPlanOrderEvent обрабатывает событие изменения SL/TP для WebSession
func (session *WebSession) handleStopPlanOrderEvent(ctx context.Context, stopPlan websocket.StopPlanOrderEvent) {
	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.active {
		return
	}

	session.logger.Info("Stop plan order event received",
		slog.String("master", session.masterAcc.Name),
		slog.Any("event", stopPlan),
	)

	// Получаем символ от мастера
	masterClient, err := mexc.NewClient(session.masterAcc, session.logger)
	if err != nil {
		session.logger.Error("Failed to create master client", slog.Any("error", err))
		return
	}

	masterOrders, err := masterClient.GetOpenStopOrders(ctx, "")
	if err != nil {
		session.logger.Error("Failed to get master open orders", slog.Any("error", err))
		return
	}

	var symbol string
	for _, order := range masterOrders {
		if order.OrderId == stopPlan.OrderId {
			symbol = order.Symbol
			break
		}
	}

	if symbol == "" {
		session.logger.Debug("Order not found in master's open orders", slog.String("orderId", stopPlan.OrderId))
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	failedCount := 0

	for _, slaveAcc := range session.slaveAccs {
		wg.Add(1)

		go func(acc models.Account) {
			defer wg.Done()

			result := processSlaveStopPlanOrderWeb(ctx, session, acc, stopPlan, symbol)

			mu.Lock()
			if result.success {
				successCount++
			} else {
				failedCount++
			}
			mu.Unlock()
		}(slaveAcc)
	}

	wg.Wait()

	userID := session.userID
	session.storage.AddLog(&models.ActivityLog{
		UserID:  &userID,
		Level:   "info",
		Action:  "sl_tp_updated",
		Message: fmt.Sprintf("%s SL/TP updated: %d/%d successful", symbol, successCount, len(session.slaveAccs)),
	})
}

// handlePositionEvent обрабатывает событие позиции для WebSession
func (session *WebSession) handlePositionEvent(ctx context.Context, pos websocket.PositionEvent) {
	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.active {
		return
	}

	// Обрабатываем только закрытие позиций (state == 3)
	if pos.State != 3 {
		return
	}

	session.logger.Info("Position closed event received",
		slog.String("master", session.masterAcc.Name),
		slog.Any("event", pos),
	)

	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	failedCount := 0

	for _, slaveAcc := range session.slaveAccs {
		wg.Add(1)

		go func(acc models.Account) {
			defer wg.Done()

			result := processSlavePositionWeb(ctx, session, acc, pos)

			mu.Lock()
			if result.success {
				successCount++
			} else {
				failedCount++
			}
			mu.Unlock()
		}(slaveAcc)
	}

	wg.Wait()

	posTypeText := "LONG"
	if pos.PositionType == 2 {
		posTypeText = "SHORT"
	}

	userID := session.userID
	session.storage.AddLog(&models.ActivityLog{
		UserID:  &userID,
		Level:   "info",
		Action:  "position_closed",
		Message: fmt.Sprintf("%s %s closed: %d/%d successful, PnL: %.2f", pos.Symbol, posTypeText, successCount, len(session.slaveAccs), pos.CloseProfitLoss),
	})
}

// handleOrderDealEvent обрабатывает событие сделки для WebSession
func (session *WebSession) handleOrderDealEvent(ctx context.Context, deal websocket.DealEvent) {
	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.active {
		return
	}

	session.logger.Info("Deal event received",
		slog.String("master", session.masterAcc.Name),
		slog.Any("event", deal),
	)

	// Логируем PnL если есть
	if deal.Profit != 0 {
		level := "info"
		if deal.Profit < 0 {
			level = "warning"
		}

		userID := session.userID
		session.storage.AddLog(&models.ActivityLog{
			UserID:  &userID,
			Level:   level,
			Action:  "deal_executed",
			Message: fmt.Sprintf("%s: PnL %.2f USDT", deal.Symbol, deal.Profit),
		})
	}
}
