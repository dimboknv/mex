package copytrading

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"

	"tg_mexc/internal/mexc"
	models2 "tg_mexc/internal/models"

	"golang.org/x/sync/errgroup"
)

type TradeStorage interface {
	CreateTrade(ctx context.Context, trade models2.Trade) (int, error)
	AddTradeDetail(ctx context.Context, detail models2.TradeDetail) error
	UpdateTradeStatus(ctx context.Context, tradeID int, status string, errorMsg string) error
}

type LogStorage interface {
	AddLog(ctx context.Context, log models2.ActivityLog) error
}

type UserStorage interface {
	GetMasterAccount(userID int) (models2.Account, error)
	GetSlaveAccounts(userID int, includeInactive bool) ([]models2.Account, error)
}

type StopOrderCache interface {
	GetStopOrderSymbol(userID int, orderID string) (string, error)
	SaveStopOrder(userID int, orderID string, symbol string) error
	SaveStopOrders(userID int, orders map[string]string) error
}

// Engine - core механизм копирования
type Engine struct {
	logStorage     LogStorage
	tradeStorage   TradeStorage
	userStorage    UserStorage
	stopOrderCache StopOrderCache
	logger         *slog.Logger
	dryRun         bool
}

func NewEngine(
	logStorage LogStorage,
	tradeStorage TradeStorage,
	userStorage UserStorage,
	stopOrderCache StopOrderCache,
	logger *slog.Logger,
	dryRun bool,
) *Engine {
	return &Engine{
		logStorage:     logStorage,
		tradeStorage:   tradeStorage,
		userStorage:    userStorage,
		stopOrderCache: stopOrderCache,
		logger:         logger,
		dryRun:         dryRun,
	}
}

// saveTrade сохраняет результаты сделки в storage (если есть)
func (e *Engine) saveTrade(ctx context.Context, record models2.Trade, result ExecutionResult) error {
	tradeID, err := e.tradeStorage.CreateTrade(ctx, record)
	if err != nil {
		return fmt.Errorf("failed to create trade record: %w", err)
	}

	for _, r := range result.Results {
		status := "success"
		if !r.Success {
			status = "failed"
		}

		err = errors.Join(e.tradeStorage.AddTradeDetail(ctx, models2.TradeDetail{
			TradeID:   tradeID,
			AccountID: r.AccountID,
			Status:    status,
			Error:     r.Error,
			OrderID:   r.OrderID,
			LatencyMs: int(r.LatencyMs),
		}))
	}

	if err != nil {
		return fmt.Errorf("failed to save trade details: %w", err)
	}

	status := "completed"
	if result.IsFullFailure() {
		status = "failed"
	} else if result.IsPartialSuccess() {
		status = "partial"
	}
	if err := e.tradeStorage.UpdateTradeStatus(ctx, tradeID, status, ""); err != nil {
		e.logger.Error("Failed to update trade status", slog.Any("error", err))
	}

	return e.saveLog(ctx, "info", record, result)
}

// saveLog сохраняет только лог активности (для операций без trade записи)
func (e *Engine) saveLog(ctx context.Context, level string, record models2.Trade, result ExecutionResult) error {
	logRecord := models2.ActivityLog{
		UserID:    &record.UserID,
		Level:     level,
		Action:    record.Action,
		Message:   fmt.Sprintf("%s %s: %d/%d successful", record.Symbol, GetSideText(record.Side), result.SuccessCount, result.TotalCount),
		CreatedAt: time.Time{},
	}

	if err := e.logStorage.AddLog(ctx, logRecord); err != nil {
		return fmt.Errorf("failed to add activity log: %w", err)
	}

	return nil
}

func (e *Engine) getSlaves(userID int) ([]models2.Account, error) {
	if _, err := e.userStorage.GetMasterAccount(userID); err != nil {
		return nil, fmt.Errorf("failed to get master account: %w", err)
	}

	slaveAccounts, err := e.userStorage.GetSlaveAccounts(userID, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get slave accounts: %w", err)
	}

	return slaveAccounts, nil
}

func (e *Engine) execute(userID int, fn func(acc models2.Account) AccountResult) (ExecutionResult, error) {
	slaveAccounts, err := e.getSlaves(userID)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("failed to get slave accounts: %w", err)
	}

	result := ExecutionResult{
		TotalCount: len(slaveAccounts),
		Results:    make([]AccountResult, 0, len(slaveAccounts)),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, slaveAcc := range slaveAccounts {
		wg.Add(1)
		go func(acc models2.Account) {
			defer wg.Done()

			startTime := time.Now()
			accResult := fn(acc)
			accResult.LatencyMs = time.Since(startTime).Milliseconds()

			mu.Lock()
			if accResult.Success {
				result.SuccessCount++
			} else {
				result.FailedCount++
			}
			result.Results = append(result.Results, accResult)
			mu.Unlock()
		}(slaveAcc)
	}

	wg.Wait()

	return result, nil
}

// OpenPosition открывает позицию на всех slave аккаунтах
func (e *Engine) OpenPosition(ctx context.Context, userID int, req OpenPositionRequest) (ExecutionResult, error) {
	result, err := e.execute(userID, func(acc models2.Account) AccountResult {
		return e.processOpenPosition(ctx, acc, req)
	})
	if err != nil {
		return ExecutionResult{}, err
	}

	record := models2.Trade{
		UserID:   userID,
		Symbol:   req.Symbol,
		Side:     req.Side,
		Volume:   int(req.Volume),
		Leverage: req.Leverage,
		Action:   "open_position",
	}
	if err := e.saveTrade(ctx, record, result); err != nil {
		return ExecutionResult{}, fmt.Errorf("failed to save trade: %w", err)
	}

	return result, nil
}

// processOpenPosition обрабатывает открытие позиции для одного аккаунта
func (e *Engine) processOpenPosition(ctx context.Context, acc models2.Account, req OpenPositionRequest) AccountResult {
	result := AccountResult{
		AccountID:   acc.ID,
		AccountName: acc.Name,
		Success:     false,
	}

	client, err := mexc.NewClient(acc, e.logger)
	if err != nil {
		e.logger.Error("Failed to create client",
			slog.String("slave", acc.Name),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	// Получаем текущий leverage для этого аккаунта
	currentLeverage, err := client.GetLeverageForSide(ctx, req.Symbol, req.Side)
	if err != nil {
		e.logger.Error("Failed to get leverage",
			slog.String("slave", acc.Name),
			slog.String("symbol", req.Symbol),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	if e.dryRun {
		e.logger.Info("DRY_RUN - Would place order",
			slog.String("slave", acc.Name),
			slog.String("symbol", req.Symbol),
			slog.Int("side", req.Side),
			slog.Float64("volume", req.Volume),
			slog.Int("leverage", currentLeverage),
			slog.Float64("stopLoss", req.StopLossPrice))
		result.Success = true
		return result
	}

	// Открываем позицию
	var orderID string
	if req.StopLossPrice > 0 {
		orderID, err = client.PlaceOrder(ctx, req.Symbol, req.Side, int(req.Volume), currentLeverage, req.StopLossPrice)
	} else {
		orderID, err = client.PlaceOrder(ctx, req.Symbol, req.Side, int(req.Volume), currentLeverage)
	}

	if err != nil {
		e.logger.Error("Failed to place order",
			slog.String("slave", acc.Name),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	e.logger.Info("Order placed successfully",
		slog.String("slave", acc.Name),
		slog.String("order_id", orderID),
		slog.Int("leverage", currentLeverage))

	result.Success = true
	result.OrderID = orderID

	return result
}

// ClosePosition закрывает позицию на всех slave аккаунтах
func (e *Engine) ClosePosition(ctx context.Context, userID int, req ClosePositionRequest) (ExecutionResult, error) {
	result, err := e.execute(userID, func(acc models2.Account) AccountResult {
		return e.processClosePosition(ctx, acc, req)
	})
	if err != nil {
		return ExecutionResult{}, err
	}

	record := models2.Trade{
		UserID: userID,
		Symbol: req.Symbol,
		Action: "close_position",
	}
	if err := e.saveTrade(ctx, record, result); err != nil {
		return ExecutionResult{}, fmt.Errorf("failed to save trade: %w", err)
	}

	return result, nil
}

// processClosePosition обрабатывает закрытие позиции для одного аккаунта
func (e *Engine) processClosePosition(ctx context.Context, acc models2.Account, req ClosePositionRequest) AccountResult {
	result := AccountResult{
		AccountID:   acc.ID,
		AccountName: acc.Name,
		Success:     false,
	}

	client, err := mexc.NewClient(acc, e.logger)
	if err != nil {
		e.logger.Error("Failed to create client",
			slog.String("slave", acc.Name),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	if e.dryRun {
		e.logger.Info("DRY_RUN - Would close position",
			slog.String("slave", acc.Name),
			slog.String("symbol", req.Symbol))
		result.Success = true
		return result
	}

	err = client.ClosePosition(ctx, req.Symbol)
	if err != nil {
		e.logger.Error("Failed to close position",
			slog.String("slave", acc.Name),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	e.logger.Info("Position closed successfully",
		slog.String("slave", acc.Name),
		slog.String("symbol", req.Symbol))

	result.Success = true

	return result
}

// PlacePlanOrder устанавливает SL/TP на всех slave аккаунтах
func (e *Engine) PlacePlanOrder(ctx context.Context, userID int, req PlacePlanOrderRequest) (ExecutionResult, error) {
	result, err := e.execute(userID, func(acc models2.Account) AccountResult {
		return e.processPlacePlanOrder(ctx, acc, req)
	})
	if err != nil {
		return ExecutionResult{}, err
	}

	record := models2.Trade{
		UserID: userID,
		Symbol: req.Symbol,
		Action: "place_plan_order",
	}
	if err := e.saveTrade(ctx, record, result); err != nil {
		return ExecutionResult{}, fmt.Errorf("failed to save trade: %w", err)
	}

	return result, nil
}

// processPlacePlanOrder обрабатывает установку SL/TP для одного аккаунта
func (e *Engine) processPlacePlanOrder(ctx context.Context, acc models2.Account, req PlacePlanOrderRequest) AccountResult {
	result := AccountResult{
		AccountID:   acc.ID,
		AccountName: acc.Name,
		Success:     false,
	}

	client, err := mexc.NewClient(acc, e.logger)
	if err != nil {
		e.logger.Error("Failed to create client",
			slog.String("slave", acc.Name),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	if e.dryRun {
		e.logger.Info("DRY_RUN - Would set SL/TP",
			slog.String("slave", acc.Name),
			slog.String("symbol", req.Symbol),
			slog.Float64("sl", req.StopLossPrice),
			slog.Float64("tp", req.TakeProfitPrice))
		result.Success = true
		return result
	}

	err = client.PlacePlanOrder(ctx, req.Symbol, req.StopLossPrice, req.TakeProfitPrice)
	if err != nil {
		e.logger.Error("Failed to set SL/TP",
			slog.String("slave", acc.Name),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	e.logger.Info("SL/TP set successfully",
		slog.String("slave", acc.Name),
		slog.String("symbol", req.Symbol))

	result.Success = true

	return result
}

// ChangePlanPrice обновляет SL/TP на всех slave аккаунтах
func (e *Engine) ChangePlanPrice(ctx context.Context, userID int, req ChangePlanPriceRequest) (ExecutionResult, error) {
	orderID := strconv.Itoa(req.StopPlanOrderID)

	// 1. Если symbol уже передан в request (WebSocket mode) - используем его
	symbol := req.Symbol

	// 2. Если нет - ищем в кэше
	if symbol == "" && e.stopOrderCache != nil {
		cachedSymbol, err := e.stopOrderCache.GetStopOrderSymbol(userID, orderID)
		if err == nil && cachedSymbol != "" {
			symbol = cachedSymbol
			e.logger.Info("Stop order symbol found in cache",
				slog.String("order_id", orderID),
				slog.String("symbol", symbol))
		}
	}

	// 3. Fallback - API вызов к master account
	if symbol == "" {
		masterAccount, err := e.userStorage.GetMasterAccount(userID)
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("failed to get master account: %w", err)
		}

		masterClient, err := mexc.NewClient(masterAccount, e.logger)
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("failed to create master client: %w", err)
		}

		masterOrders, err := masterClient.GetOpenStopOrders(ctx, "")
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("failed to get master open orders: %w", err)
		}

		// Ищем нужный order и кэшируем все orders
		// ВАЖНО: ключ кэша - это order.Id (int), не order.OrderId (string)
		ordersToCache := make(map[string]string)
		for _, order := range masterOrders {
			ordersToCache[strconv.Itoa(order.Id)] = order.Symbol
			if order.Id == req.StopPlanOrderID {
				symbol = order.Symbol
			}
		}

		// Сохраняем в кэш для будущих запросов
		if e.stopOrderCache != nil && len(ordersToCache) > 0 {
			if err := e.stopOrderCache.SaveStopOrders(userID, ordersToCache); err != nil {
				e.logger.Warn("Failed to cache stop orders", slog.Any("error", err))
			}
		}
	}

	if symbol == "" {
		return ExecutionResult{}, fmt.Errorf("stop order %d not found", req.StopPlanOrderID)
	}

	result, err := e.execute(userID, func(acc models2.Account) AccountResult {
		return e.processChangePlanPrice(ctx, symbol, acc, req)
	})
	if err != nil {
		return ExecutionResult{}, err
	}

	record := models2.Trade{
		UserID: userID,
		Symbol: symbol,
		Action: "change_plan_price",
	}
	if err := e.saveTrade(ctx, record, result); err != nil {
		return ExecutionResult{}, fmt.Errorf("failed to save trade: %w", err)
	}

	return result, nil
}

// processChangePlanPrice обрабатывает обновление SL/TP для одного аккаунта
func (e *Engine) processChangePlanPrice(ctx context.Context, symbol string, acc models2.Account, req ChangePlanPriceRequest) AccountResult {
	result := AccountResult{
		AccountID:   acc.ID,
		AccountName: acc.Name,
		Success:     false,
	}

	client, err := mexc.NewClient(acc, e.logger)
	if err != nil {
		e.logger.Error("Failed to create client",
			slog.String("slave", acc.Name),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	// Получаем открытые стоп-ордера для этого аккаунта
	slaveOrders, err := client.GetOpenStopOrders(ctx, symbol)
	if err != nil {
		e.logger.Error("Failed to get slave open orders",
			slog.String("slave", acc.Name),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	if len(slaveOrders) == 0 {
		e.logger.Debug("No stop orders found for slave",
			slog.String("slave", acc.Name),
			slog.String("symbol", symbol))
		result.Success = true // Не считаем ошибкой
		return result
	}

	slaveOrder := slaveOrders[0]

	if e.dryRun {
		e.logger.Info("DRY_RUN - Would update SL/TP",
			slog.String("slave", acc.Name),
			slog.String("symbol", symbol),
			slog.Float64("sl", req.StopLossPrice))
		result.Success = true
		return result
	}

	changeReq := models2.ChangePlanPriceRequest{
		StopPlanOrderID:   slaveOrder.Id,
		LossTrend:         req.LossTrend,
		ProfitTrend:       req.ProfitTrend,
		StopLossReverse:   req.StopLossReverse,
		TakeProfitReverse: req.TakeProfitReverse,
		StopLossPrice:     req.StopLossPrice,
	}

	if err = client.ChangePlanPrice(ctx, changeReq); err != nil {
		e.logger.Error("Failed to update SL/TP",
			slog.String("slave", acc.Name),
			slog.String("symbol", symbol),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	e.logger.Info("SL/TP updated successfully",
		slog.String("slave", acc.Name),
		slog.String("symbol", symbol))

	result.Success = true

	return result
}

// ChangeLeverage изменяет leverage на всех slave аккаунтах
func (e *Engine) ChangeLeverage(ctx context.Context, userID int, req ChangeLeverageRequest) (ExecutionResult, error) {
	result, err := e.execute(userID, func(acc models2.Account) AccountResult {
		return e.processChangeLeverage(ctx, acc, req)
	})
	if err != nil {
		return ExecutionResult{}, err
	}

	record := models2.Trade{
		UserID:   userID,
		Symbol:   req.Symbol,
		Leverage: req.Leverage,
		Action:   "change_leverage",
	}
	if err := e.saveTrade(ctx, record, result); err != nil {
		return ExecutionResult{}, fmt.Errorf("failed to save trade: %w", err)
	}

	return result, nil
}

// processChangeLeverage обрабатывает изменение leverage для одного аккаунта
func (e *Engine) processChangeLeverage(ctx context.Context, acc models2.Account, req ChangeLeverageRequest) AccountResult {
	result := AccountResult{
		AccountID:   acc.ID,
		AccountName: acc.Name,
		Success:     false,
	}

	client, err := mexc.NewClient(acc, e.logger)
	if err != nil {
		e.logger.Error("Failed to create client",
			slog.String("slave", acc.Name),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	if e.dryRun {
		e.logger.Info("DRY_RUN - Would change leverage",
			slog.String("slave", acc.Name),
			slog.String("symbol", req.Symbol),
			slog.Int("leverage", req.Leverage))
		result.Success = true
		return result
	}

	changeReq := mexc.ChangeLeverageRequest{
		Symbol:       req.Symbol,
		Leverage:     req.Leverage,
		OpenType:     req.OpenType,
		PositionType: req.PositionType,
	}

	if err = client.ChangeLeverage(ctx, changeReq); err != nil {
		e.logger.Error("Failed to change leverage",
			slog.String("slave", acc.Name),
			slog.String("symbol", req.Symbol),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	e.logger.Info("Leverage changed successfully",
		slog.String("slave", acc.Name),
		slog.String("symbol", req.Symbol),
		slog.Int("leverage", req.Leverage))

	result.Success = true

	return result
}

// CancelStopOrder отменяет стоп-ордера на всех slave аккаунтах
func (e *Engine) CancelStopOrder(ctx context.Context, userID int, orderIDs []int) (ExecutionResult, error) {
	orderIDsStrings := make([]string, 0, len(orderIDs))
	for _, orderID := range orderIDs {
		orderIDsStrings = append(orderIDsStrings, strconv.Itoa(orderID))
	}

	// 1. Пытаемся найти symbols в кэше
	symbols := make([]string, 0, len(orderIDs))
	missingOrderIDs := make([]string, 0)

	if e.stopOrderCache != nil {
		for _, orderID := range orderIDsStrings {
			cachedSymbol, err := e.stopOrderCache.GetStopOrderSymbol(userID, orderID)
			if err == nil && cachedSymbol != "" {
				symbols = append(symbols, cachedSymbol)
				e.logger.Info("Stop order symbol found in cache",
					slog.String("order_id", orderID),
					slog.String("symbol", cachedSymbol))
			} else {
				missingOrderIDs = append(missingOrderIDs, orderID)
			}
		}
	} else {
		missingOrderIDs = orderIDsStrings
	}

	// 2. Если есть missing - идем по API
	if len(missingOrderIDs) > 0 {
		masterAccount, err := e.userStorage.GetMasterAccount(userID)
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("failed to get master account: %w", err)
		}

		masterClient, err := mexc.NewClient(masterAccount, e.logger)
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("failed to create master client: %w", err)
		}

		masterOrders, err := masterClient.GetOpenStopOrders(ctx, "")
		if err != nil {
			return ExecutionResult{}, fmt.Errorf("failed to get master open orders: %w", err)
		}

		// Кэшируем все orders и находим нужные symbols
		// ВАЖНО: ключ кэша - это order.Id (int), не order.OrderId (string)
		ordersToCache := make(map[string]string)
		for _, order := range masterOrders {
			orderIDStr := strconv.Itoa(order.Id)
			ordersToCache[orderIDStr] = order.Symbol
			if slices.Contains(missingOrderIDs, orderIDStr) {
				symbols = append(symbols, order.Symbol)
			}
		}

		// Сохраняем в кэш
		if e.stopOrderCache != nil && len(ordersToCache) > 0 {
			if err := e.stopOrderCache.SaveStopOrders(userID, ordersToCache); err != nil {
				e.logger.Warn("Failed to cache stop orders", slog.Any("error", err))
			}
		}
	}

	if len(symbols) == 0 {
		return ExecutionResult{}, fmt.Errorf("order not found")
	}

	var result ExecutionResult
	var mu sync.Mutex

	errg, c := errgroup.WithContext(ctx)
	for _, sym := range symbols {
		symbol := sym // capture для goroutine
		errg.Go(func() error {
			res, err := e.execute(userID, func(acc models2.Account) AccountResult {
				return e.processCancelStopOrder(c, acc, CancelStopOrderRequest{Symbol: symbol})
			})
			if err != nil {
				return err
			}

			mu.Lock()
			result.Results = append(result.Results, res.Results...)
			result.SuccessCount += res.SuccessCount
			result.FailedCount += res.FailedCount
			result.TotalCount += res.TotalCount
			mu.Unlock()

			return nil
		})
	}

	if err := errg.Wait(); err != nil {
		return ExecutionResult{}, fmt.Errorf("failed to cancel stop orders: %w", err)
	}

	record := models2.Trade{
		UserID: userID,
		Action: "cancel_stop_order",
	}
	if err := e.saveTrade(ctx, record, result); err != nil {
		return ExecutionResult{}, fmt.Errorf("failed to save trade: %w", err)
	}

	return result, nil
}

// processCancelStopOrder обрабатывает отмену стоп-ордера для одного аккаунта
func (e *Engine) processCancelStopOrder(ctx context.Context, acc models2.Account, req CancelStopOrderRequest) AccountResult {
	result := AccountResult{
		AccountID:   acc.ID,
		AccountName: acc.Name,
		Success:     false,
	}

	client, err := mexc.NewClient(acc, e.logger)
	if err != nil {
		e.logger.Error("Failed to create client",
			slog.String("slave", acc.Name),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	// Получаем открытые стоп-ордера
	slaveOrders, err := client.GetOpenStopOrders(ctx, req.Symbol)
	if err != nil {
		e.logger.Error("Failed to get slave open orders",
			slog.String("slave", acc.Name),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	if len(slaveOrders) == 0 {
		e.logger.Debug("No stop orders found for slave",
			slog.String("slave", acc.Name),
			slog.String("symbol", req.Symbol))
		result.Success = true // Не считаем ошибкой
		return result
	}

	if e.dryRun {
		e.logger.Info("DRY_RUN - Would cancel stop loss",
			slog.String("slave", acc.Name),
			slog.String("symbol", req.Symbol))
		result.Success = true
		return result
	}

	// Отменяем первый найденный стоп-ордер
	slaveOrder := slaveOrders[0]
	if err = client.CancelStopOrder(ctx, int64(slaveOrder.Id)); err != nil {
		e.logger.Error("Failed to cancel stop loss",
			slog.String("slave", acc.Name),
			slog.String("symbol", req.Symbol),
			slog.Any("error", err))
		result.Error = err.Error()
		return result
	}

	e.logger.Info("Stop loss cancelled successfully",
		slog.String("slave", acc.Name),
		slog.String("symbol", req.Symbol))

	result.Success = true

	return result
}

// CancelStopOrderBySymbol отменяет стоп-ордера на всех slave аккаунтах по символу
func (e *Engine) CancelStopOrderBySymbol(ctx context.Context, userID int, symbol string) (ExecutionResult, error) {
	result, err := e.execute(userID, func(acc models2.Account) AccountResult {
		return e.processCancelStopOrder(ctx, acc, CancelStopOrderRequest{Symbol: symbol})
	})
	if err != nil {
		return ExecutionResult{}, err
	}

	record := models2.Trade{
		UserID: userID,
		Symbol: symbol,
		Action: "cancel_stop_order",
	}
	if err := e.saveTrade(ctx, record, result); err != nil {
		return ExecutionResult{}, fmt.Errorf("failed to save trade: %w", err)
	}

	return result, nil
}
