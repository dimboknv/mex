package copytrading

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"tg_mexc/models"
	"tg_mexc/services/mexc"
	"tg_mexc/services/websocket"
	"tg_mexc/storage"
	"time"
)

type Service struct {
	storage  *storage.Storage
	logger   *slog.Logger
	dryRun   bool
	mu       sync.RWMutex
	sessions map[int64]*Session // chatID -> Session
}

type Session struct {
	chatID       int64
	wsClient     *websocket.Client
	masterAcc    models.Account
	slaveAccs    []models.Account
	logger       *slog.Logger
	storage      *storage.Storage
	dryRun       bool
	ignoreFees   bool // Игнорировать комиссию при копировании
	active       bool
	mu           sync.RWMutex
	eventChannel chan string
}

func New(storage *storage.Storage, logger *slog.Logger, dryRun bool) *Service {
	return &Service{
		storage:  storage,
		logger:   logger,
		dryRun:   dryRun,
		sessions: make(map[int64]*Session),
	}
}

func (s *Service) Start(chatID int64, ignoreFees bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session, exists := s.sessions[chatID]; exists && session.active {
		return "", fmt.Errorf("⏹️ Copy trading уже запущен")
	}

	masterAcc, err := s.storage.GetMasterAccount(chatID)
	if err != nil {
		return "", fmt.Errorf("мастер аккаунт не установлен. Используй /set_master <name>")
	}

	slaveAccs, err := s.storage.GetSlaveAccounts(chatID)
	if err != nil {
		return "", fmt.Errorf("ошибка получения дочерних аккаунтов: %w", err)
	}

	if len(slaveAccs) == 0 {
		return "", fmt.Errorf("нет дочерних аккаунтов для копирования")
	}

	session := &Session{
		chatID:       chatID,
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
		return "", fmt.Errorf("ошибка подключения к WebSocket: %w", err)
	}

	session.wsClient = wsClient
	s.sessions[chatID] = session

	s.logger.Info("Copy trading started",
		slog.Int64("chat_id", chatID),
		slog.String("master", masterAcc.Name),
		slog.Int("slaves", len(slaveAccs)))

	modeInfo := "\n\n⚠️ Режим: PRODUCTION (реальные сделки)"
	if s.dryRun {
		modeInfo = "\n\n🔍 Режим: DRY_RUN (только логирование)"
	}

	feeInfo := ""
	if ignoreFees {
		feeInfo = "\n🔓 Игнорирование комиссий: ВКЛ"
	} else {
		feeInfo = "\n🔒 Игнорирование комиссий: ВЫКЛ (только аккаунты без комиссии)"
	}

	return fmt.Sprintf("✅ Copy trading запущен!%s%s\n\nМастер: %s\nДочерних аккаунтов: %d",
		modeInfo, feeInfo, masterAcc.Name, len(slaveAccs)), nil
}

func (s *Service) Stop(chatID int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[chatID]
	if !exists || !session.active {
		return "", fmt.Errorf("⏹️ Copy trading не запущен")
	}

	if session.wsClient != nil {
		session.wsClient.Disconnect()
	}

	session.active = false
	close(session.eventChannel)
	delete(s.sessions, chatID)

	s.logger.Info("Copy trading stopped", slog.Int64("chat_id", chatID))

	return "✅ Copy trading остановлен", nil
}

func (s *Service) IsActive(chatID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[chatID]

	return exists && session.active
}

func (s *Service) GetStatus(chatID int64) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[chatID]
	if !exists || !session.active {
		return "⏹️ Copy trading не запущен"
	}

	modeInfo := "\n\n⚠️ Режим: PRODUCTION (реальные сделки)"
	if session.dryRun {
		modeInfo = "\n\n🔍 Режим: DRY_RUN (только логирование)"
	}

	return fmt.Sprintf("▶️ Copy trading активен%s\n\nМастер: %s\nДочерних аккаунтов: %d",
		modeInfo, session.masterAcc.Name, len(session.slaveAccs))
}

type accountOrderResult struct {
	success bool
	detail  string
}

func (session *Session) processSlaveOrder(ctx context.Context, slaveAcc models.Account, order websocket.OrderEvent, isOpenOrder bool, sideText string) accountOrderResult {
	result := accountOrderResult{success: false}

	// Проверяем disabled статус для операций открытия позиции (только если ignoreFees = false)
	if isOpenOrder && !session.ignoreFees {
		session.logger.Info("Skipping disabled account for open order",
			slog.String("slave", slaveAcc.Name),
			slog.Bool("ignoreFees", session.ignoreFees))

		result.detail = fmt.Sprintf("🛑 %s: пропущен (disabled)", slaveAcc.Name)
		result.success = true // Считаем success чтобы не было ошибок

		return result
	}

	client, err := mexc.NewClient(slaveAcc, session.logger)
	if err != nil {
		session.logger.Error("Failed to create client",
			slog.String("slave", slaveAcc.Name),
			slog.Any("error", err))

		result.detail = fmt.Sprintf("❌ %s: ошибка создания клиента", slaveAcc.Name)

		return result
	}

	if isOpenOrder {
		// ОТКРЫТИЕ ПОЗИЦИИ (side 1 или 3)
		// Получаем текущий leverage для этого аккаунта и символа
		currentLeverage, err := client.GetLeverageForSide(ctx, order.Symbol, order.Side)
		if err != nil {
			session.logger.Error("Failed to get leverage",
				slog.String("slave", slaveAcc.Name),
				slog.String("symbol", order.Symbol),
				slog.Any("error", err))

			result.detail = fmt.Sprintf("❌ %s: ошибка получения leverage", slaveAcc.Name)

			return result
		}

		// Проверяем, есть ли StopOrderEvent
		var stopLossPrice float64
		stopLossInfo := ""
		if order.StopOrderEvent != nil && order.StopOrderEvent.StopLossPrice > 0 {
			stopLossPrice = order.StopOrderEvent.StopLossPrice
			stopLossInfo = fmt.Sprintf(", SL: %.1f", stopLossPrice)
			session.logger.Info("📊 Opening position with Stop Loss",
				slog.String("slave", slaveAcc.Name),
				slog.String("symbol", order.Symbol),
				slog.Int("leverage", currentLeverage),
				slog.Int("master_leverage", order.Leverage),
				slog.Float64("stopLoss", stopLossPrice))
		} else {
			session.logger.Info("📊 Opening position",
				slog.String("slave", slaveAcc.Name),
				slog.String("symbol", order.Symbol),
				slog.Int("leverage", currentLeverage),
				slog.Int("master_leverage", order.Leverage))
		}

		if session.dryRun {
			session.logger.Info("🔍 DRY_RUN - Would place order",
				slog.String("slave", slaveAcc.Name),
				slog.String("symbol", order.Symbol),
				slog.Int("side", order.Side),
				slog.Float64("volume", order.Vol),
				slog.Int("leverage", currentLeverage),
				slog.Float64("stopLoss", stopLossPrice))

			result.success = true
			result.detail = fmt.Sprintf("✅ %s: открыл %s %.0f контрактов, leverage %dx%s (DRY RUN)",
				slaveAcc.Name, sideText, order.Vol, currentLeverage, stopLossInfo)
		} else {
			// Вызываем PlaceOrder с StopLoss если он есть
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

				result.detail = fmt.Sprintf("❌ %s: ошибка - %v", slaveAcc.Name, err)
			} else {
				session.logger.Info("✅ Order copied successfully",
					slog.String("slave", slaveAcc.Name),
					slog.Int("leverage", currentLeverage),
					slog.String("order_id", orderID),
					slog.Float64("stopLoss", stopLossPrice))

				result.success = true
				result.detail = fmt.Sprintf("✅ %s: открыл %s %.0f контрактов, leverage %dx%s, ID: %s",
					slaveAcc.Name, sideText, order.Vol, currentLeverage, stopLossInfo, orderID)
			}
		}
	} else {
		// ЗАКРЫТИЕ ПОЗИЦИИ (side 2 или 4)
		session.logger.Info("📊 Closing position",
			slog.String("slave", slaveAcc.Name),
			slog.String("symbol", order.Symbol),
			slog.String("type", sideText))

		if session.dryRun {
			session.logger.Info("🔍 DRY_RUN - Would close position",
				slog.String("slave", slaveAcc.Name),
				slog.String("symbol", order.Symbol))

			result.success = true
			result.detail = fmt.Sprintf("✅ %s: закрыл %s (DRY RUN)", slaveAcc.Name, sideText)
		} else {
			err = client.ClosePosition(ctx, order.Symbol)
			if err != nil {
				session.logger.Error("Failed to close position",
					slog.String("slave", slaveAcc.Name),
					slog.Any("error", err))

				result.detail = fmt.Sprintf("❌ %s: ошибка - %v", slaveAcc.Name, err)
			} else {
				session.logger.Info("✅ Position closed successfully",
					slog.String("slave", slaveAcc.Name))

				result.success = true
				result.detail = fmt.Sprintf("✅ %s: закрыл %s", slaveAcc.Name, sideText)
			}
		}
	}

	time.Sleep(100 * time.Millisecond)

	return result
}

func (session *Session) handleOrderEvent(ctx context.Context, order websocket.OrderEvent) {
	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.active {
		return
	}

	session.logger.Info("Order event received",
		slog.String("master", session.masterAcc.Name),
		slog.Any("order", order),
	)

	// Обрабатываем все типы ордеров:
	// side 1: open long - открываем long
	// side 2: close short - закрываем short
	// side 3: open short - открываем short
	// side 4: close long - закрываем long

	// Определяем тип операции и позиции
	var isOpenOrder bool
	var sideText, actionText string

	switch order.Side {
	case 1:
		isOpenOrder = true
		sideText = "LONG"
		actionText = "открыл позицию"
	case 2:
		isOpenOrder = false
		sideText = "SHORT"
		actionText = "закрыл позицию"
	case 3:
		isOpenOrder = true
		sideText = "SHORT"
		actionText = "открыл позицию"
	case 4:
		isOpenOrder = false
		sideText = "LONG"
		actionText = "закрыл позицию"
	default:
		session.logger.Debug("Unknown order side", slog.Int("side", order.Side))
		return
	}

	eventMsg := fmt.Sprintf("📊 Мастер %s:\n%s %s\nОбъем: %.0f\nКопирую на %d аккаунтов...",
		actionText, order.Symbol, sideText, order.Vol, len(session.slaveAccs))

	select {
	case session.eventChannel <- eventMsg:
	default:
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	failedCount := 0
	var accountDetails []string

	for _, slaveAcc := range session.slaveAccs {
		wg.Add(1)

		go func(acc models.Account) {
			defer wg.Done()

			result := session.processSlaveOrder(ctx, acc, order, isOpenOrder, sideText)

			mu.Lock()
			if result.success {
				successCount++
			} else {
				failedCount++
			}

			accountDetails = append(accountDetails, result.detail)
			mu.Unlock()
		}(slaveAcc)
	}

	wg.Wait()

	// Формируем детальное сообщение
	detailsText := ""
	for _, detail := range accountDetails {
		detailsText += "\n" + detail
	}

	// Отправляем результат
	var resultMsg string
	if session.dryRun {
		if isOpenOrder {
			resultMsg = fmt.Sprintf("🔍 DRY_RUN - Позиция открыта:\n\n%s %s\nОбъем мастера: %.0f контрактов\n\n📊 Детали по аккаунтам:%s\n\n✅ Успешно: %d/%d\n❌ Ошибки: %d\n\n⚠️ РЕЖИМ ТЕСТИРОВАНИЯ",
				order.Symbol, sideText, order.Vol,
				detailsText,
				successCount, len(session.slaveAccs), failedCount)
		} else {
			resultMsg = fmt.Sprintf("🔍 DRY_RUN - Позиция закрыта:\n\n%s %s\nОбъем мастера: %.0f контрактов\n\n📊 Детали по аккаунтам:%s\n\n✅ Успешно: %d/%d\n❌ Ошибки: %d\n\n⚠️ РЕЖИМ ТЕСТИРОВАНИЯ",
				order.Symbol, sideText, order.Vol,
				detailsText,
				successCount, len(session.slaveAccs), failedCount)
		}
	} else {
		if isOpenOrder {
			resultMsg = fmt.Sprintf("✅ Позиция открыта:\n\n%s %s\nОбъем мастера: %.0f контрактов\n\n📊 Детали по аккаунтам:%s\n\n✅ Успешно: %d/%d\n❌ Ошибки: %d",
				order.Symbol, sideText, order.Vol,
				detailsText,
				successCount, len(session.slaveAccs), failedCount)
		} else {
			resultMsg = fmt.Sprintf("✅ Позиция закрыта:\n\n%s %s\nОбъем мастера: %.0f контрактов\n\n📊 Детали по аккаунтам:%s\n\n✅ Успешно: %d/%d\n❌ Ошибки: %d",
				order.Symbol, sideText, order.Vol,
				detailsText,
				successCount, len(session.slaveAccs), failedCount)
		}
	}

	select {
	case session.eventChannel <- resultMsg:
	default:
	}
}

type accountStopResult struct {
	success bool
	detail  string
}

func (session *Session) processSlaveStopOrder(ctx context.Context, slaveAcc models.Account, stop websocket.StopOrderEvent) accountStopResult {
	result := accountStopResult{success: false}

	client, err := mexc.NewClient(slaveAcc, session.logger)
	if err != nil {
		session.logger.Error("Failed to create client",
			slog.String("slave", slaveAcc.Name),
			slog.Any("error", err))

		result.detail = fmt.Sprintf("❌ %s: ошибка создания клиента", slaveAcc.Name)

		return result
	}

	if session.dryRun {
		session.logger.Info("🔍 DRY_RUN - Would set SL/TP",
			slog.String("slave", slaveAcc.Name),
			slog.String("symbol", stop.Symbol),
			slog.Float64("sl", stop.StopLossPrice),
			slog.Float64("tp", stop.TakeProfitPrice))

		result.success = true
		result.detail = fmt.Sprintf("✅ %s: SL/TP установлен (DRY RUN)", slaveAcc.Name)
	} else {
		err = client.SetStopLoss(ctx, stop.Symbol, stop.StopLossPrice, stop.TakeProfitPrice)
		if err != nil {
			session.logger.Error("Failed to set SL/TP",
				slog.String("slave", slaveAcc.Name),
				slog.Any("error", err))

			result.detail = fmt.Sprintf("❌ %s: ошибка - %v", slaveAcc.Name, err)
		} else {
			session.logger.Info("✅ SL/TP set successfully",
				slog.String("slave", slaveAcc.Name))

			result.success = true
			result.detail = fmt.Sprintf("✅ %s: SL/TP установлен", slaveAcc.Name)
		}
	}

	time.Sleep(100 * time.Millisecond)

	return result
}

// handleStopOrderEvent обрабатывает событие установки SL/TP и копирует его на дочерние аккаунты
func (session *Session) handleStopOrderEvent(ctx context.Context, stop websocket.StopOrderEvent) {
	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.active {
		return
	}

	// Логируем событие
	session.logger.Info("Stop order event received",
		slog.String("master", session.masterAcc.Name),
		slog.Any("event", stop),
	)

	eventMsg := fmt.Sprintf("🎯 Мастер установил SL/TP:\n%s\nSL: %.2f\nTP: %.2f\nКопирую на %d аккаунтов...",
		stop.Symbol, stop.StopLossPrice, stop.TakeProfitPrice, len(session.slaveAccs))

	select {
	case session.eventChannel <- eventMsg:
	default:
	}

	// Копируем на дочерние аккаунты
	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	failedCount := 0
	var accountDetails []string

	for _, slaveAcc := range session.slaveAccs {
		wg.Add(1)

		go func(acc models.Account) {
			defer wg.Done()

			result := session.processSlaveStopOrder(ctx, acc, stop)

			mu.Lock()
			if result.success {
				successCount++
			} else {
				failedCount++
			}

			accountDetails = append(accountDetails, result.detail)
			mu.Unlock()
		}(slaveAcc)
	}

	wg.Wait()

	// Формируем результат
	detailsText := ""
	for _, detail := range accountDetails {
		detailsText += "\n" + detail
	}

	var resultMsg string
	if session.dryRun {
		resultMsg = fmt.Sprintf("🔍 DRY_RUN - SL/TP установлен:\n\n%s\nSL: %.2f\nTP: %.2f\n\n📊 Детали по аккаунтам:%s\n\n✅ Успешно: %d/%d\n❌ Ошибки: %d",
			stop.Symbol, stop.StopLossPrice, stop.TakeProfitPrice,
			detailsText,
			successCount, len(session.slaveAccs), failedCount)
	} else {
		resultMsg = fmt.Sprintf("✅ SL/TP установлен:\n\n%s\nSL: %.2f\nTP: %.2f\n\n📊 Детали по аккаунтам:%s\n\n✅ Успешно: %d/%d\n❌ Ошибки: %d",
			stop.Symbol, stop.StopLossPrice, stop.TakeProfitPrice,
			detailsText,
			successCount, len(session.slaveAccs), failedCount)
	}

	select {
	case session.eventChannel <- resultMsg:
	default:
	}
}

type accountStopPlanResult struct {
	success bool
	detail  string
}

func (session *Session) processSlaveStopPlanOrder(ctx context.Context, slaveAcc models.Account, stopPlan websocket.StopPlanOrderEvent, symbol string) accountStopPlanResult {
	result := accountStopPlanResult{success: false}

	client, err := mexc.NewClient(slaveAcc, session.logger)
	if err != nil {
		session.logger.Error("Failed to create client",
			slog.String("slave", slaveAcc.Name),
			slog.Any("error", err))

		result.detail = fmt.Sprintf("❌ %s: ошибка создания клиента", slaveAcc.Name)

		return result
	}

	slaveAccOrders, err := client.GetOpenStopOrders(ctx, symbol)
	if err != nil {
		session.logger.Error("Failed to get slave open orders",
			slog.Any("slave", slaveAcc),
			slog.Any("error", err))

		result.detail = fmt.Sprintf("❌ %s: ошибка получения ордеров", slaveAcc.Name)

		return result
	}

	if len(slaveAccOrders) == 0 {
		session.logger.Debug("Order not found in slave's open orders",
			slog.String("slave", slaveAcc.Name),
			slog.String("orderId", stopPlan.OrderId))

		result.detail = fmt.Sprintf("⚠️ %s: ордер не найден", slaveAcc.Name)

		return result
	}

	slaveAccOrder := slaveAccOrders[0]

	if session.dryRun {
		session.logger.Info("🔍 DRY_RUN - Would update SL/TP",
			slog.String("slave", slaveAcc.Name),
			slog.String("symbol", symbol))

		result.success = true
		result.detail = fmt.Sprintf("✅ %s: SL/TP обновлен (DRY RUN)", slaveAcc.Name)
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
				slog.String("symbol", symbol),
				slog.Any("error", err))

			result.detail = fmt.Sprintf("❌ %s: ошибка - %v", slaveAcc.Name, err)
		} else {
			session.logger.Info("✅ SL/TP updated successfully",
				slog.String("slave", slaveAcc.Name),
				slog.String("symbol", symbol))

			result.success = true
			result.detail = fmt.Sprintf("✅ %s: SL/TP обновлен", slaveAcc.Name)
		}
	}

	time.Sleep(100 * time.Millisecond)

	return result
}

// handleStopPlanOrderEvent обрабатывает событие изменения SL/TP и копирует его на дочерние аккаунты
func (session *Session) handleStopPlanOrderEvent(ctx context.Context, stopPlan websocket.StopPlanOrderEvent) {
	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.active {
		return
	}

	session.logger.Info("Stop plan order event received",
		slog.String("master", session.masterAcc.Name),
		slog.Any("event", stopPlan),
	)

	// Получаем ордер мастера по ID чтобы узнать символ
	masterClient, err := mexc.NewClient(session.masterAcc, session.logger)
	if err != nil {
		session.logger.Error("Failed to create master client",
			slog.String("master", session.masterAcc.Name),
			slog.Any("error", err))

		return
	}

	// Получаем открытые ордера мастера
	masterOrders, err := masterClient.GetOpenStopOrders(ctx, "")
	if err != nil {
		session.logger.Error("Failed to get master open orders",
			slog.String("master", session.masterAcc.Name),
			slog.Any("error", err))

		return
	}

	// Ищем ордер по ID
	var masterOrder *models.StopOrder
	var symbol string
	for i, order := range masterOrders {
		if order.OrderId == stopPlan.OrderId {
			symbol = order.Symbol
			masterOrder = &masterOrders[i]

			break
		}
	}

	if masterOrder == nil {
		session.logger.Debug("Order not found in master's open orders",
			slog.String("master", session.masterAcc.Name),
			slog.String("orderId", stopPlan.OrderId))

		return
	}

	// Логируем событие с символом
	session.logger.Info("Stop plan order event received",
		slog.String("master", session.masterAcc.Name),
		slog.String("symbol", masterOrder.Symbol),
		slog.String("orderId", stopPlan.OrderId),
		slog.Float64("sl", stopPlan.StopLossPrice))

	// Копируем на дочерние аккаунты
	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	failedCount := 0
	var accountDetails []string

	for _, slaveAcc := range session.slaveAccs {
		wg.Add(1)

		go func(acc models.Account) {
			defer wg.Done()

			result := session.processSlaveStopPlanOrder(ctx, acc, stopPlan, symbol)

			mu.Lock()
			if result.success {
				successCount++
			} else {
				failedCount++
			}

			accountDetails = append(accountDetails, result.detail)
			mu.Unlock()
		}(slaveAcc)
	}

	wg.Wait()

	// Формируем результат
	detailsText := ""
	for _, detail := range accountDetails {
		detailsText += "\n" + detail
	}

	var resultMsg string
	if session.dryRun {
		resultMsg = fmt.Sprintf("🔍 DRY_RUN - SL/TP обновлен:\n\n%s\nSL: %.2f\n\n📊 Детали по аккаунтам:%s\n\n✅ Успешно: %d/%d\n❌ Ошибки: %d",
			symbol, stopPlan.StopLossPrice,
			detailsText,
			successCount, len(session.slaveAccs), failedCount)
	} else {
		resultMsg = fmt.Sprintf("✅ SL/TP обновлен:\n\n%s\nSL: %.2f\n\n📊 Детали по аккаунтам:%s\n\n✅ Успешно: %d/%d\n❌ Ошибки: %d",
			symbol, stopPlan.StopLossPrice,
			detailsText,
			successCount, len(session.slaveAccs), failedCount)
	}

	select {
	case session.eventChannel <- resultMsg:
	default:
	}
}

// handleOrderDealEvent обрабатывает событие сделки (логирование)
func (session *Session) handleOrderDealEvent(ctx context.Context, deal websocket.DealEvent) {
	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.active {
		return
	}

	session.logger.Info("Deal event received",
		slog.String("master", session.masterAcc.Name),
		slog.Any("event", deal),
	)

	// Логируем событие сделки
	sideText := "BUY"
	if deal.Side == 2 || deal.Side == 3 {
		sideText = "SELL"
	}

	// Отправляем уведомление пользователю только если есть прибыль/убыток
	if deal.Profit != 0 {
		profitEmoji := "📈"
		if deal.Profit < 0 {
			profitEmoji = "📉"
		}

		eventMsg := fmt.Sprintf("%s Мастер: сделка исполнена\n%s %s\nОбъем: %.0f\nЦена: %.2f\nPnL: %.2f USDT",
			profitEmoji, deal.Symbol, sideText, deal.Vol, deal.Price, deal.Profit)

		select {
		case session.eventChannel <- eventMsg:
		default:
		}
	}
}

type accountPositionResult struct {
	success bool
	detail  string
}

func (session *Session) processSlavePosition(ctx context.Context, slaveAcc models.Account, pos websocket.PositionEvent) accountPositionResult {
	result := accountPositionResult{success: false}

	client, err := mexc.NewClient(slaveAcc, session.logger)
	if err != nil {
		session.logger.Error("Failed to create client",
			slog.String("slave", slaveAcc.Name),
			slog.Any("error", err))

		result.detail = fmt.Sprintf("❌ %s: ошибка создания клиента", slaveAcc.Name)

		return result
	}

	if session.dryRun {
		session.logger.Info("🔍 DRY_RUN - Would close position",
			slog.String("slave", slaveAcc.Name),
			slog.String("symbol", pos.Symbol))

		result.success = true
		result.detail = fmt.Sprintf("✅ %s: закрыл %s (DRY RUN)", slaveAcc.Name, pos.Symbol)
	} else {
		err = client.ClosePosition(ctx, pos.Symbol)
		if err != nil {
			session.logger.Error("Failed to close position",
				slog.String("slave", slaveAcc.Name),
				slog.Any("error", err))

			result.detail = fmt.Sprintf("❌ %s: ошибка - %v", slaveAcc.Name, err)
		} else {
			session.logger.Info("✅ Position closed successfully",
				slog.String("slave", slaveAcc.Name))

			result.success = true
			result.detail = fmt.Sprintf("✅ %s: закрыл %s", slaveAcc.Name, pos.Symbol)
		}
	}

	time.Sleep(100 * time.Millisecond)

	return result
}

// handlePositionEvent обрабатывает событие позиции (для отслеживания закрытия)
func (session *Session) handlePositionEvent(ctx context.Context, pos websocket.PositionEvent) {
	session.mu.Lock()
	defer session.mu.Unlock()

	if !session.active {
		return
	}

	session.logger.Info("Position closed event received",
		slog.String("master", session.masterAcc.Name),
		slog.Any("event", pos),
	)

	// Обрабатываем только закрытие позиций (state == 3)
	if pos.State != 3 {
		return
	}

	posTypeText := "LONG"
	if pos.PositionType == 2 {
		posTypeText = "SHORT"
	}

	eventMsg := fmt.Sprintf("📊 Мастер закрыл позицию:\n%s %s\nPnL: %.2f USDT\nЗакрываю на %d аккаунтов...",
		pos.Symbol, posTypeText, pos.CloseProfitLoss, len(session.slaveAccs))

	select {
	case session.eventChannel <- eventMsg:
	default:
	}

	// Закрываем позиции на дочерних аккаунтах
	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	failedCount := 0
	var accountDetails []string

	for _, slaveAcc := range session.slaveAccs {
		wg.Add(1)

		go func(acc models.Account) {
			defer wg.Done()

			result := session.processSlavePosition(ctx, acc, pos)

			mu.Lock()
			if result.success {
				successCount++
			} else {
				failedCount++
			}

			accountDetails = append(accountDetails, result.detail)
			mu.Unlock()
		}(slaveAcc)
	}

	wg.Wait()

	// Формируем детальное сообщение
	detailsText := ""
	for _, detail := range accountDetails {
		detailsText += "\n" + detail
	}

	// Отправляем результат
	var resultMsg string
	if session.dryRun {
		resultMsg = fmt.Sprintf("🔍 DRY_RUN - Позиция закрыта:\n\n%s %s\nPnL мастера: %.2f USDT\n\n📊 Детали по аккаунтам:%s\n\n✅ Успешно: %d/%d\n❌ Ошибки: %d\n\n⚠️ РЕЖИМ ТЕСТИРОВАНИЯ - реальные сделки не закрываются",
			pos.Symbol, posTypeText, pos.CloseProfitLoss,
			detailsText,
			successCount, len(session.slaveAccs), failedCount)
	} else {
		resultMsg = fmt.Sprintf("✅ Позиция закрыта:\n\n%s %s\nPnL мастера: %.2f USDT\n\n📊 Детали по аккаунтам:%s\n\n✅ Успешно: %d/%d\n❌ Ошибки: %d",
			pos.Symbol, posTypeText, pos.CloseProfitLoss,
			detailsText,
			successCount, len(session.slaveAccs), failedCount)
	}

	select {
	case session.eventChannel <- resultMsg:
	default:
	}
}

func (s *Service) GetEventChannel(chatID int64) <-chan string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[chatID]
	if !exists {
		return nil
	}

	return session.eventChannel
}
