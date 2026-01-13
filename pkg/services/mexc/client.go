package mexc

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"tg_mexc/pkg/models"
	"tg_mexc/pkg/services/httpmiddleware"

	"github.com/google/uuid"
)

const (
	baseURL = "https://www.mexc.com"

	// API endpoints
	orderCreateEndpoint        = "/api/platform/futures/api/v1/private/order/create"
	positionsEndpoint          = "/api/platform/futures/api/v1/private/position/open_positions"
	accountAssetsEndpoint      = "/api/platform/futures/api/v1/private/account/assets"
	leverageEndpoint           = "/api/platform/futures/api/v1/private/position/leverage"
	stopLossEndpoint           = "/api/platform/futures/api/v1/private/planorder/place"
	stopLossCancelEndpoint     = "/api/platform/futures/api/v1/private/stoporder/cancel"
	stopLossOpenOrdersEndpoint = "/api/platform/futures/api/v1/private/stoporder/open_orders"
	changePlanPriceEndpoint    = "/api/platform/futures/api/v1/private/stoporder/change_plan_price"
	openOrdersEndpoint         = "/api/platform/futures/api/v1/private/order/list/open_orders"
	tieredFeeRateEndpoint      = "/api/platform/futures/api/v1/private/account/tiered_fee_rate/v2"
	changeLeverageEndpoint     = "/api/platform/futures/api/v1/private/position/change_leverage"
)

// Client - клиент для работы с MEXC API
type Client struct {
	account    models.Account
	httpClient *http.Client
	logger     *slog.Logger
	baseURL    string
}

// NewClient создает новый MEXC клиент для аккаунта
func NewClient(account models.Account, logger *slog.Logger) (*Client, error) {
	jar, _ := cookiejar.New(nil)

	// Базовый transport
	baseTransport := httpmiddleware.DefaultTransport()

	// Если есть прокси - настраиваем
	if account.Proxy != "" {
		proxyURL, err := url.Parse(account.Proxy)
		if err != nil {
			logger.Error("Invalid proxy",
				slog.String("account", account.Name),
				slog.Any("error", err))
		} else {
			baseTransport.Proxy = http.ProxyURL(proxyURL)

			logger.Info("Using proxy",
				slog.String("account", account.Name),
				slog.String("proxy", account.Proxy))
		}
	}

	httpClient := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		Transport: httpmiddleware.Wrap(
			baseTransport,
			httpmiddleware.RequestGetBodySetter,
			httpmiddleware.Logger(logger, -1),
		),
	}

	client := &Client{
		account:    account,
		httpClient: httpClient,
		logger:     logger,
		baseURL:    baseURL,
	}

	// Устанавливаем cookies
	client.setCookies()

	return client, nil
}

// setCookies устанавливает cookies из аккаунта
func (c *Client) setCookies() {
	u, _ := url.Parse(c.baseURL)
	var httpCookies []*http.Cookie

	for key, value := range c.account.Cookies {
		// Sanitize cookie value - remove quotes and other invalid characters
		sanitizedValue := sanitizeCookieValue(value)

		httpCookies = append(httpCookies, &http.Cookie{
			Name:   key,
			Value:  sanitizedValue,
			Domain: ".mexc.com",
			Path:   "/",
		})
	}

	c.httpClient.Jar.SetCookies(u, httpCookies)
}

// sanitizeCookieValue удаляет недопустимые символы из значения cookie
func sanitizeCookieValue(value string) string {
	// Убираем окружающие кавычки если есть
	value = strings.Trim(value, "\"")

	// Заменяем внутренние кавычки на escaped версию
	value = strings.ReplaceAll(value, "\"", "\\\"")

	// Убираем другие потенциально проблемные символы
	value = strings.ReplaceAll(value, "\n", "")
	value = strings.ReplaceAll(value, "\r", "")

	return value
}

// generateSignature генерирует MD5 подпись для запроса
func (c *Client) generateSignature(timestamp int64, body []byte) string {
	timeStr := fmt.Sprintf("%d", timestamp)
	step1 := md5Hash(c.account.Token + timeStr)
	step1Sub := step1[7:]
	finalInput := timeStr + string(body) + step1Sub

	return md5Hash(finalInput)
}

func md5Hash(input string) string {
	hash := md5.Sum([]byte(input))
	return hex.EncodeToString(hash[:])
}

// setHeaders устанавливает заголовки для запроса
func (c *Client) setHeaders(req *http.Request, timestamp int64, signature string) {
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("Referer", c.baseURL+"/futures")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("platform", "H5-web")

	if c.account.UserAgent != "" {
		req.Header.Set("User-Agent", c.account.UserAgent)
	} else {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	}

	req.Header.Set("Authorization", c.account.Token)
	req.Header.Set("mtoken", c.account.DeviceID)
	req.Header.Set("device-id", c.account.DeviceID)
	req.Header.Set("trochilus-uid", c.account.UserID)

	req.Header.Set("language", "en-US")
	req.Header.Set("X-Language", "en-US")
	req.Header.Set("country-code", "DE")
	req.Header.Set("timezone-login", "UTC+02:00")

	if signature != "" {
		req.Header.Set("x-mxc-sign", signature)
		req.Header.Set("x-mxc-nonce", fmt.Sprintf("%d", timestamp))
	}

	traceID := fmt.Sprintf("%s-%04d", generateUUID(), timestamp%10000)
	req.Header.Set("trochilus-trace-id", traceID)

	req.Header.Set("baggage", "sentry-environment=production,sentry-release=v5.25.11")

	sentryTrace := fmt.Sprintf("%s-%s-0", generateUUID(), generateShortID())
	req.Header.Set("sentry-trace", sentryTrace)
}

func generateUUID() string {
	return uuid.New().String()
}

func generateShortID() string {
	return fmt.Sprintf("%016x", time.Now().UnixNano())
}

// PlaceOrder размещает ордер (открывает позицию)
// stopLossPrice - опциональный параметр для установки stop loss при создании ордера (передать 0 если не нужен)
func (c *Client) PlaceOrder(ctx context.Context, symbol string, side int, vol int, leverage int, stopLossPrice ...float64) (string, error) {
	timestamp := time.Now().UnixMilli()

	orderReq := models.OpenPositionRequest{
		Symbol:        symbol,
		Side:          side,
		OpenType:      1,   // 1: isolated
		Type:          "5", // "5": market order (СТРОКА!)
		Vol:           vol,
		Leverage:      leverage,
		MarketCeiling: false,
		PriceProtect:  "0",
	}

	// Добавляем stop loss если указан
	if len(stopLossPrice) > 0 && stopLossPrice[0] > 0 {
		orderReq.StopLossPrice = fmt.Sprintf("%.1f", stopLossPrice[0])
		orderReq.LossTrend = "1" // "1": latest price (СТРОКА!)
	}

	body, _ := json.Marshal(orderReq)
	signature := c.generateSignature(timestamp, body)

	apiURL := c.baseURL + orderCreateEndpoint

	req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(body))
	c.setHeaders(req, timestamp, signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("PlaceOrder failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var orderResp models.OrderResponse
	json.Unmarshal(respBody, &orderResp)

	if !orderResp.Success {
		c.logger.Error("PlaceOrder API error",
			slog.String("account", c.account.Name),
			slog.Int("code", orderResp.Code),
			slog.String("message", orderResp.Message))

		return "", fmt.Errorf("order failed: %s", orderResp.Message)
	}

	c.logger.Info("✅ PlaceOrder success",
		slog.String("account", c.account.Name),
		slog.String("orderId", orderResp.Data.OrderID))

	return orderResp.Data.OrderID, nil
}

// GetPositions получает позиции
func (c *Client) GetPositions(ctx context.Context, symbol string) ([]models.Position, error) {
	timestamp := time.Now().UnixMilli()

	apiURL := c.baseURL + positionsEndpoint
	if symbol != "" {
		apiURL += "?symbol=" + symbol
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
	c.setHeaders(req, timestamp, "")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("GetPositions failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool              `json:"success"`
		Data    []models.Position `json:"data"`
	}

	json.Unmarshal(body, &result)

	if !result.Success {
		c.logger.Error("GetPositions API error",
			slog.String("account", c.account.Name),
			slog.String("response", string(body)))

		return nil, fmt.Errorf("API error: %s", string(body))
	}

	return result.Data, nil
}

// GetBalance получает баланс
func (c *Client) GetBalance(ctx context.Context) ([]models.Balance, error) {
	timestamp := time.Now().UnixMilli()

	apiURL := c.baseURL + accountAssetsEndpoint

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
	c.setHeaders(req, timestamp, "")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("GetBalance failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool             `json:"success"`
		Data    []models.Balance `json:"data"`
	}

	json.Unmarshal(body, &result)

	if !result.Success {
		c.logger.Error("GetBalance API error",
			slog.String("account", c.account.Name),
			slog.String("response", string(body)))

		return nil, fmt.Errorf("API error: %s", string(body))
	}

	return result.Data, nil
}

// GetLeverage получает текущий leverage для символа
func (c *Client) GetLeverage(ctx context.Context, symbol string) ([]models.LeverageInfo, error) {
	timestamp := time.Now().UnixMilli()

	apiURL := c.baseURL + leverageEndpoint + "?symbol=" + symbol

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
	c.setHeaders(req, timestamp, "")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("GetLeverage failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool                  `json:"success"`
		Data    []models.LeverageInfo `json:"data"`
	}

	json.Unmarshal(body, &result)

	if !result.Success {
		c.logger.Error("GetLeverage API error",
			slog.String("account", c.account.Name),
			slog.String("response", string(body)))

		return nil, fmt.Errorf("API error: %s", string(body))
	}

	return result.Data, nil
}

// GetLeverageForSide получает leverage для конкретной стороны (long/short)
func (c *Client) GetLeverageForSide(ctx context.Context, symbol string, side int) (int, error) {
	leverages, err := c.GetLeverage(ctx, symbol)
	if err != nil {
		return 0, err
	}

	// Определяем нужный positionType на основе side
	// side 1 = open long -> positionType 1
	// side 3 = open short -> positionType 2
	positionType := 1
	if side == 3 {
		positionType = 2
	}

	// Ищем leverage для нужного типа позиции
	for _, lev := range leverages {
		if lev.PositionType == positionType {
			c.logger.Debug("Found leverage for position",
				slog.String("account", c.account.Name),
				slog.String("symbol", symbol),
				slog.Int("positionType", positionType),
				slog.Int("leverage", lev.Leverage))

			return lev.Leverage, nil
		}
	}

	return 0, fmt.Errorf("leverage not found for positionType %d", positionType)
}

// ClosePosition закрывает позицию
func (c *Client) ClosePosition(ctx context.Context, symbol string) error {
	c.logger.Info("Closing position",
		slog.String("account", c.account.Name),
		slog.String("symbol", symbol))

	positions, err := c.GetPositions(ctx, symbol)
	if err != nil {
		return err
	}

	if len(positions) == 0 {
		c.logger.Info("No positions to close",
			slog.String("account", c.account.Name),
			slog.String("symbol", symbol))

		return nil
	}

	for _, pos := range positions {
		if pos.Symbol == symbol && pos.HoldVol > 0 {
			closeSide := 4 // close long
			posTypeText := "LONG"
			if pos.PositionType == 2 {
				closeSide = 2 // close short
				posTypeText = "SHORT"
			}

			c.logger.Info("Closing position",
				slog.String("account", c.account.Name),
				slog.String("symbol", symbol),
				slog.String("type", posTypeText),
				slog.Float64("vol", pos.HoldVol))

			// Закрываем позицию с указанием positionId
			timestamp := time.Now().UnixMilli()

			orderReq := models.ClosePositionRequest{
				Symbol:       symbol,
				OpenType:     1, // 1: isolated
				PositionID:   pos.PositionID,
				Leverage:     pos.Leverage,
				Type:         5, // 5: market order (ЧИСЛО!)
				Vol:          int(pos.HoldVol),
				Side:         closeSide,
				PriceProtect: "0",
			}

			body, _ := json.Marshal(orderReq)
			signature := c.generateSignature(timestamp, body)

			apiURL := c.baseURL + orderCreateEndpoint

			req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(body))
			c.setHeaders(req, timestamp, signature)

			resp, err := c.httpClient.Do(req)
			if err != nil {
				c.logger.Error("ClosePosition failed",
					slog.String("account", c.account.Name),
					slog.Any("error", err))

				return err
			}
			defer resp.Body.Close()

			respBody, _ := io.ReadAll(resp.Body)

			var orderResp models.OrderResponse
			json.Unmarshal(respBody, &orderResp)

			if !orderResp.Success {
				c.logger.Error("ClosePosition API error",
					slog.String("account", c.account.Name),
					slog.Int("code", orderResp.Code),
					slog.String("message", orderResp.Message))

				return fmt.Errorf("close position failed: %s", orderResp.Message)
			}

			c.logger.Info("✅ ClosePosition success",
				slog.String("account", c.account.Name),
				slog.String("orderId", orderResp.Data.OrderID))
		}
	}

	return nil
}

// PlacePlanOrder устанавливает Stop Loss и Take Profit для позиции
func (c *Client) PlacePlanOrder(ctx context.Context, symbol string, stopLossPrice, takeProfitPrice float64) error {
	timestamp := time.Now().UnixMilli()

	stopLossReq := models.StopLossRequest{
		Symbol:          symbol,
		StopLossPrice:   stopLossPrice,
		TakeProfitPrice: takeProfitPrice,
	}

	body, _ := json.Marshal(stopLossReq)
	signature := c.generateSignature(timestamp, body)

	apiURL := c.baseURL + stopLossEndpoint

	req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(body))
	c.setHeaders(req, timestamp, signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("SetStopLoss failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool   `json:"success"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	json.Unmarshal(respBody, &result)

	if !result.Success {
		c.logger.Error("SetStopLoss API error",
			slog.String("account", c.account.Name),
			slog.Int("code", result.Code),
			slog.String("message", result.Message))

		return fmt.Errorf("set SL/TP failed: %s", result.Message)
	}

	c.logger.Info("✅ SetStopLoss success",
		slog.String("account", c.account.Name),
		slog.String("symbol", symbol),
		slog.Float64("sl", stopLossPrice),
		slog.Float64("tp", takeProfitPrice))

	return nil
}

// GetOpenStopOrders получает список открытых стоп-ордеров
func (c *Client) GetOpenStopOrders(ctx context.Context, symbol string) ([]models.StopOrder, error) {
	timestamp := time.Now().UnixMilli()

	apiURL := c.baseURL + stopLossOpenOrdersEndpoint
	if symbol != "" {
		apiURL += "?symbol=" + symbol
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
	c.setHeaders(req, timestamp, "")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("GetOpenStopOrders failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool               `json:"success"`
		Data    []models.StopOrder `json:"data"`
	}

	json.Unmarshal(body, &result)

	if !result.Success {
		c.logger.Error("GetOpenStopOrders API error",
			slog.String("account", c.account.Name),
			slog.String("response", string(body)))

		return nil, fmt.Errorf("API error: %s", string(body))
	}

	return result.Data, nil
}

// CancelStopOrder отменяет стоп-ордер по ID
func (c *Client) CancelStopOrder(ctx context.Context, stopPlanOrderID int64) error {
	timestamp := time.Now().UnixMilli()

	cancelItems := []models.StopOrderCancelItem{
		{StopPlanOrderID: stopPlanOrderID},
	}

	body, _ := json.Marshal(cancelItems)
	signature := c.generateSignature(timestamp, body)

	apiURL := c.baseURL + stopLossCancelEndpoint

	req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(body))
	c.setHeaders(req, timestamp, signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("CancelStopLoss failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool   `json:"success"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	json.Unmarshal(respBody, &result)

	if !result.Success {
		c.logger.Error("CancelStopLoss API error",
			slog.String("account", c.account.Name),
			slog.Int("code", result.Code),
			slog.String("message", result.Message))

		return fmt.Errorf("cancel SL/TP failed: %s", result.Message)
	}

	c.logger.Info("✅ CancelStopLoss success",
		slog.String("account", c.account.Name),
		slog.Int64("stopPlanOrderId", stopPlanOrderID))

	return nil
}

// CancelAllStopLossBySymbol отменяет все стоп-ордера для символа
func (c *Client) CancelAllStopLossBySymbol(ctx context.Context, symbol string) error {
	stopOrders, err := c.GetOpenStopOrders(ctx, symbol)
	if err != nil {
		return err
	}

	if len(stopOrders) == 0 {
		c.logger.Info("No stop orders to cancel",
			slog.String("account", c.account.Name),
			slog.String("symbol", symbol))

		return nil
	}

	// for _, order := range stopOrders {
	// 	if order.Symbol == symbol && order.State == 1 { // state 1 = active
	// 		c.logger.Info("Canceling stop order",
	// 			slog.String("account", c.account.Name),
	// 			slog.String("symbol", symbol),
	// 			slog.String("orderId", order.OrderId))
	//
	// 		err := c.CancelStopLoss(order.OrderId)
	// 		if err != nil {
	// 			c.logger.Error("Failed to cancel stop order",
	// 				slog.String("account", c.account.Name),
	// 				slog.Int64("orderId", order.ID),
	// 				slog.Any("error", err))
	// 			// Продолжаем отменять остальные
	// 		}
	//
	// 		time.Sleep(100 * time.Millisecond)
	// 	}
	// }

	return nil
}

// ChangePlanPrice изменяет цену stop loss для существующего ордера
func (c *Client) ChangePlanPrice(ctx context.Context, req1 models.ChangePlanPriceRequest) error {
	timestamp := time.Now().UnixMilli()

	body, _ := json.Marshal(req1)
	signature := c.generateSignature(timestamp, body)

	apiURL := c.baseURL + changePlanPriceEndpoint

	req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(body))
	c.setHeaders(req, timestamp, signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("ChangeStopLoss failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool   `json:"success"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	json.Unmarshal(respBody, &result)

	if !result.Success {
		c.logger.Error("ChangeStopLoss API error",
			slog.String("account", c.account.Name),
			slog.Int("code", result.Code),
			slog.String("message", result.Message))

		return fmt.Errorf("change SL/TP failed: %s", result.Message)
	}

	c.logger.Info("✅ ChangeStopLoss success",
		slog.String("account", c.account.Name))

	return nil
}

// GetOpenOrders получает список открытых ордеров
func (c *Client) GetOpenOrders(ctx context.Context, pageNum, pageSize int) ([]models.OpenOrder, error) {
	timestamp := time.Now().UnixMilli()

	// Default values
	if pageNum < 1 {
		pageNum = 1
	}

	if pageSize < 1 {
		pageSize = 20
	}

	if pageSize > 100 {
		pageSize = 100
	}

	apiURL := fmt.Sprintf("%s%s?page_num=%d&page_size=%d", c.baseURL, openOrdersEndpoint, pageNum, pageSize)

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
	c.setHeaders(req, timestamp, "")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("GetOpenOrders failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool               `json:"success"`
		Data    []models.OpenOrder `json:"data"`
	}

	json.Unmarshal(body, &result)

	if !result.Success {
		c.logger.Error("GetOpenOrders API error",
			slog.String("account", c.account.Name),
			slog.String("response", string(body)))

		return nil, fmt.Errorf("API error: %s", string(body))
	}

	return result.Data, nil
}

// GetTieredFeeRate получает информацию о комиссионных ставках
func (c *Client) GetTieredFeeRate(ctx context.Context, symbol string) (*models.TieredFeeRateResponse, error) {
	timestamp := time.Now().UnixMilli()

	apiURL := c.baseURL + tieredFeeRateEndpoint
	if symbol != "" {
		apiURL += "?symbol=" + symbol
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, http.NoBody)
	c.setHeaders(req, timestamp, "")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("GetTieredFeeRate failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool                         `json:"success"`
		Data    models.TieredFeeRateResponse `json:"data"`
	}

	json.Unmarshal(body, &result)

	if !result.Success {
		c.logger.Error("GetTieredFeeRate API error",
			slog.String("account", c.account.Name),
			slog.String("response", string(body)))

		return nil, fmt.Errorf("API error: %s", string(body))
	}

	return &result.Data, nil
}

// cleanRawRequest удаляет технические поля подписи из raw запроса
func cleanRawRequest(reqBody []byte) ([]byte, error) {
	var rawReq map[string]any
	if err := json.Unmarshal(reqBody, &rawReq); err != nil {
		return nil, err
	}

	// Удаляем поля с подписью master аккаунта
	delete(rawReq, "p0")
	delete(rawReq, "k0")
	delete(rawReq, "chash")
	delete(rawReq, "mtoken")
	delete(rawReq, "ts")
	delete(rawReq, "mhash")

	return json.Marshal(rawReq)
}

// PlaceOrderRaw выполняет запрос на создание ордера с raw данными из browser mirror
func (c *Client) PlaceOrderRaw(ctx context.Context, reqBody []byte) (string, error) {
	timestamp := time.Now().UnixMilli()

	body, err := cleanRawRequest(reqBody)
	if err != nil {
		return "", err
	}

	signature := c.generateSignature(timestamp, body)

	apiURL := c.baseURL + orderCreateEndpoint

	req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(body))
	c.setHeaders(req, timestamp, signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("PlaceOrderRaw failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var orderResp models.OrderResponse
	json.Unmarshal(respBody, &orderResp)

	if !orderResp.Success {
		c.logger.Error("PlaceOrderRaw API error",
			slog.String("account", c.account.Name),
			slog.Int("code", orderResp.Code),
			slog.String("message", orderResp.Message))

		return "", fmt.Errorf("order failed: %s", orderResp.Message)
	}

	c.logger.Info("✅ PlaceOrderRaw success",
		slog.String("account", c.account.Name),
		slog.String("orderId", orderResp.Data.OrderID))

	return orderResp.Data.OrderID, nil
}

// SetStopLossRaw устанавливает SL/TP с raw данными из browser mirror
func (c *Client) SetStopLossRaw(ctx context.Context, reqBody []byte) error {
	timestamp := time.Now().UnixMilli()

	body, err := cleanRawRequest(reqBody)
	if err != nil {
		return err
	}

	signature := c.generateSignature(timestamp, body)

	apiURL := c.baseURL + stopLossEndpoint

	req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(body))
	c.setHeaders(req, timestamp, signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("SetStopLossRaw failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool   `json:"success"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	json.Unmarshal(respBody, &result)

	if !result.Success {
		c.logger.Error("SetStopLossRaw API error",
			slog.String("account", c.account.Name),
			slog.Int("code", result.Code),
			slog.String("message", result.Message))

		return fmt.Errorf("set SL/TP failed: %s", result.Message)
	}

	c.logger.Info("✅ SetStopLossRaw success",
		slog.String("account", c.account.Name))

	return nil
}

// ChangeStopLossRaw изменяет цену stop loss с raw данными из browser mirror
func (c *Client) ChangeStopLossRaw(ctx context.Context, reqBody []byte) error {
	timestamp := time.Now().UnixMilli()

	body, err := cleanRawRequest(reqBody)
	if err != nil {
		return err
	}

	signature := c.generateSignature(timestamp, body)

	apiURL := c.baseURL + changePlanPriceEndpoint

	req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(body))
	c.setHeaders(req, timestamp, signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("ChangeStopLossRaw failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool   `json:"success"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	json.Unmarshal(respBody, &result)

	if !result.Success {
		c.logger.Error("ChangeStopLossRaw API error",
			slog.String("account", c.account.Name),
			slog.Int("code", result.Code),
			slog.String("message", result.Message))

		return fmt.Errorf("change SL/TP failed: %s", result.Message)
	}

	c.logger.Info("✅ ChangeStopLossRaw success",
		slog.String("account", c.account.Name))

	return nil
}

// CancelStopLossRaw отменяет stop order с raw данными из browser mirror
func (c *Client) CancelStopLossRaw(ctx context.Context, reqBody []byte) error {
	timestamp := time.Now().UnixMilli()

	body, err := cleanRawRequest(reqBody)
	if err != nil {
		return err
	}

	signature := c.generateSignature(timestamp, body)

	apiURL := c.baseURL + stopLossCancelEndpoint

	req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(body))
	c.setHeaders(req, timestamp, signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("CancelStopLossRaw failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool   `json:"success"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	json.Unmarshal(respBody, &result)

	if !result.Success {
		c.logger.Error("CancelStopLossRaw API error",
			slog.String("account", c.account.Name),
			slog.Int("code", result.Code),
			slog.String("message", result.Message))

		return fmt.Errorf("cancel stop order failed: %s", result.Message)
	}

	c.logger.Info("✅ CancelStopLossRaw success",
		slog.String("account", c.account.Name))

	return nil
}

// ChangeLeverageRaw изменяет leverage с raw данными из browser mirror
func (c *Client) ChangeLeverageRaw(ctx context.Context, reqBody []byte) error {
	timestamp := time.Now().UnixMilli()

	body, err := cleanRawRequest(reqBody)
	if err != nil {
		return err
	}

	signature := c.generateSignature(timestamp, body)

	apiURL := c.baseURL + changeLeverageEndpoint

	req, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(body))
	c.setHeaders(req, timestamp, signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Error("ChangeLeverageRaw failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool   `json:"success"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	json.Unmarshal(respBody, &result)

	if !result.Success {
		c.logger.Error("ChangeLeverageRaw API error",
			slog.String("account", c.account.Name),
			slog.Int("code", result.Code),
			slog.String("message", result.Message))

		return fmt.Errorf("change leverage failed: %s", result.Message)
	}

	c.logger.Info("✅ ChangeLeverageRaw success",
		slog.String("account", c.account.Name))

	return nil
}

// ChangeLeverageRequest - запрос на изменение leverage
type ChangeLeverageRequest struct {
	Symbol       string `json:"symbol"`
	Leverage     int    `json:"leverage"`
	OpenType     int    `json:"openType"`
	PositionType int    `json:"positionType"`
}

// ChangeLeverage изменяет leverage для символа
func (c *Client) ChangeLeverage(ctx context.Context, req ChangeLeverageRequest) error {
	timestamp := time.Now().UnixMilli()

	body, _ := json.Marshal(req)
	signature := c.generateSignature(timestamp, body)

	apiURL := c.baseURL + changeLeverageEndpoint

	httpReq, _ := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(body))
	c.setHeaders(httpReq, timestamp, signature)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logger.Error("ChangeLeverage failed",
			slog.String("account", c.account.Name),
			slog.Any("error", err))

		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Success bool   `json:"success"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	json.Unmarshal(respBody, &result)

	if !result.Success {
		c.logger.Error("ChangeLeverage API error",
			slog.String("account", c.account.Name),
			slog.Int("code", result.Code),
			slog.String("message", result.Message))

		return fmt.Errorf("change leverage failed: %s", result.Message)
	}

	c.logger.Info("✅ ChangeLeverage success",
		slog.String("account", c.account.Name),
		slog.String("symbol", req.Symbol),
		slog.Int("leverage", req.Leverage),
		slog.Int("position_type", req.PositionType))

	return nil
}
