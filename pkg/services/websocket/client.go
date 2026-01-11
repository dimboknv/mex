package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"tg_mexc/pkg/models"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsURL = "wss://contract.mexc.com/edge"
)

type Message struct {
	Method  string          `json:"method,omitempty"`
	Channel string          `json:"channel,omitempty"`
	Param   json.RawMessage `json:"param,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type LoginParam struct {
	Token string `json:"token"`
}

type OrderEvent struct {
	OrderID      string  `json:"orderId"`
	Symbol       string  `json:"symbol"`
	PositionID   int64   `json:"positionId"`
	Price        float64 `json:"price"`
	Vol          float64 `json:"vol"`
	Leverage     int     `json:"leverage"`
	Side         int     `json:"side"` // 1 open long, 2 close short, 3 open short, 4 close long
	State        int     `json:"state"`
	DealVol      float64 `json:"dealVol"`
	DealAvgPrice float64 `json:"dealAvgPrice"`
	Profit       float64 `json:"profit"`
	CreateTime   int64   `json:"createTime"`
	UpdateTime   int64   `json:"updateTime"`

	StopOrderEvent *StopOrderEvent `json:"-"`
}

type PositionEvent struct {
	PositionID      int64   `json:"positionId"`
	Symbol          string  `json:"symbol"`
	HoldVol         float64 `json:"holdVol"`
	PositionType    int     `json:"positionType"` // 1 long, 2 short
	State           int     `json:"state"`        // 1 holding, 2 system custody, 3 closed
	HoldAvgPrice    float64 `json:"holdAvgPrice"`
	Pnl             float64 `json:"pnl"`
	Leverage        int     `json:"leverage"`
	CreateTime      int64   `json:"createTime"`
	UpdateTime      int64   `json:"updateTime"`
	LiquidatePrice  float64 `json:"liquidatePrice"`
	CloseProfitLoss float64 `json:"closeProfitLoss"`
	Realised        float64 `json:"realised"`
}

type StopOrderEvent struct {
	Symbol          string  `json:"symbol"`
	OrderID         string  `json:"orderId"`
	LossTrend       int     `json:"lossTrend"`
	ProfitTrend     int     `json:"profitTrend"`
	StopLossPrice   float64 `json:"stopLossPrice"`
	TakeProfitPrice float64 `json:"takeProfitPrice"`
}

type StopPlanOrderEvent struct {
	IsFinished        int     `json:"isFinished"`
	LossTrend         int     `json:"lossTrend"`
	OrderId           string  `json:"orderId"`
	ProfitTrend       int     `json:"profitTrend"`
	StopLossReverse   int     `json:"stopLossReverse"`
	TakeProfitReverse int     `json:"takeProfitReverse"`
	StopLossPrice     float64 `json:"stopLossPrice"`
}
type DealEvent struct {
	ID        string  `json:"id"`
	Symbol    string  `json:"symbol"`
	Side      int     `json:"side"`
	Vol       float64 `json:"vol"`
	Price     float64 `json:"price"`
	Fee       float64 `json:"fee"`
	Timestamp int64   `json:"timestamp"`
	Profit    float64 `json:"profit"`
	IsTaker   bool    `json:"isTaker"`
	OrderID   string  `json:"orderId"`
}

type EventHandler func(event any)

type pendingOrder struct {
	order      OrderEvent
	timer      *time.Timer
	cancelFunc context.CancelFunc
}

type Client struct {
	account models.Account
	conn    *websocket.Conn
	logger  *slog.Logger

	orderHandler         EventHandler
	positionHandler      EventHandler
	stopOrderHandler     EventHandler
	stopPlanOrderHandler EventHandler
	orderDealHandler     EventHandler

	// Для матчинга событий
	pendingOrders map[string]*pendingOrder
	pendingMu     sync.Mutex

	done   chan struct{}
	mu     sync.Mutex
	active bool
}

func New(account models.Account, logger *slog.Logger) *Client {
	return &Client{
		account:       account,
		logger:        logger,
		done:          make(chan struct{}),
		pendingOrders: make(map[string]*pendingOrder),
	}
}

func (c *Client) SetOrderHandler(handler EventHandler) {
	c.orderHandler = handler
}

func (c *Client) SetPositionHandler(handler EventHandler) {
	c.positionHandler = handler
}

func (c *Client) SetStopOrderHandler(handler EventHandler) {
	c.stopOrderHandler = handler
}

func (c *Client) SetStopPlanOrderHandler(handler EventHandler) {
	c.stopPlanOrderHandler = handler
}

func (c *Client) SetOrderDealHandler(handler EventHandler) {
	c.orderDealHandler = handler
}

func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.active {
		return fmt.Errorf("already connected")
	}

	c.logger.Info("Connecting to WebSocket", slog.String("account", c.account.Name))

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial error: %w", err)
	}

	c.conn = conn
	c.active = true
	c.done = make(chan struct{})

	go c.readMessages()
	go c.sendPings()

	time.Sleep(500 * time.Millisecond)

	if err := c.login(); err != nil {
		return errors.Join(fmt.Errorf("login error: %w", err), c.Disconnect())
	}

	return nil
}

func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.active {
		return nil
	}

	c.active = false
	close(c.done)

	if c.conn != nil {
		c.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		c.conn.Close()
		c.conn = nil
	}

	c.logger.Info("WebSocket disconnected", slog.String("account", c.account.Name))

	return nil
}

func (c *Client) IsActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.active
}

func (c *Client) login() error {
	loginParam := LoginParam{
		Token: c.account.Token,
	}

	paramJSON, _ := json.Marshal(loginParam)
	loginMsg := Message{
		Method: "login",
		Param:  paramJSON,
	}

	c.logger.Info("Authenticating WebSocket", slog.String("account", c.account.Name))

	return c.conn.WriteJSON(loginMsg)
}

func (c *Client) readMessages() {
	defer func() {
		if err := c.Disconnect(); err != nil {
			c.logger.Error("WebSocket disconnect error", slog.Any("error", err))
		}
	}()

	for {
		select {
		case <-c.done:
			return
		default:
		}

		_, message, err := c.conn.ReadMessage()
		if err != nil {
			c.logger.Error("WebSocket read error", slog.Any("error", err))
			return
		}

		c.logger.Debug("📥 WebSocket READ", slog.String("raw", string(message)))

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			c.logger.Error("Failed to unmarshal WebSocket message",
				slog.Any("error", err),
				slog.String("raw", string(message)),
			)

			continue
		}

		c.handleMessage(msg)
	}
}

// handleMessage обрабатывает сообщение
func (c *Client) handleMessage(msg Message) {
	switch msg.Channel {
	case "rs.login":
		c.logger.Info("✅ WebSocket authenticated")

	case "push.personal.order":
		var order OrderEvent
		if err := json.Unmarshal(msg.Data, &order); err != nil {
			c.logger.Error("Failed to unmarshal push.personal.order",
				slog.Any("error", err),
				slog.String("data", string(msg.Data)),
			)

			return
		}

		c.handleOrderEventMatching(order)

	case "push.personal.position":
		// var pos PositionEvent
		// if json.Unmarshal(msg.Data, &pos) == nil {
		// 	if c.positionHandler != nil {
		// 		c.positionHandler(pos)
		// 	}
		// }

	case "push.personal.stop.order":
		var stop StopOrderEvent
		if err := json.Unmarshal(msg.Data, &stop); err != nil {
			c.logger.Error("Failed to unmarshal push.personal.stop.order",
				slog.Any("error", err),
				slog.String("data", string(msg.Data)),
			)

			return
		}

		c.handleStopOrderEventMatching(stop)

	case "push.personal.stop.planorder":
		var stopPlan StopPlanOrderEvent
		if err := json.Unmarshal(msg.Data, &stopPlan); err != nil {
			c.logger.Error("Failed to unmarshal push.personal.stop.planorder",
				slog.Any("error", err),
				slog.String("data", string(msg.Data)),
			)

			return
		}

		if c.stopPlanOrderHandler != nil {
			c.stopPlanOrderHandler(stopPlan)
		}

	case "push.personal.order.deal":
		var deal DealEvent
		if err := json.Unmarshal(msg.Data, &deal); err != nil {
			c.logger.Error("Failed to unmarshal push.personal.order.deal",
				slog.Any("error", err),
				slog.String("data", string(msg.Data)),
			)

			return
		}

		if c.orderDealHandler != nil {
			c.orderDealHandler(deal)
		}

	case "pong", "push.personal.asset", "push.personal.liquidate.risk", "rs.personal.filter", "rs.sub.order", "rs.sub.position":
		return

	default:
		return
	}
}

// handleOrderEventMatching обрабатывает событие ордера с ожиданием stop order
func (c *Client) handleOrderEventMatching(order OrderEvent) {
	c.logger.Debug("📦 Received order event",
		slog.String("orderId", order.OrderID),
		slog.String("symbol", order.Symbol),
		slog.Int("side", order.Side))

	c.pendingMu.Lock()

	// Создаем контекст для таймера
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)

	// Сохраняем заказ в pending
	pending := &pendingOrder{
		order:      order,
		cancelFunc: cancel,
	}

	// Создаем таймер на 1 секунду
	pending.timer = time.AfterFunc(1*time.Second, func() {
		c.pendingMu.Lock()
		defer c.pendingMu.Unlock()

		// Проверяем, что заказ все еще в pending
		if p, exists := c.pendingOrders[order.OrderID]; exists {
			c.logger.Debug("⏰ Timeout waiting for stop order, dispatching order without stop",
				slog.String("orderId", order.OrderID))

			// Удаляем из pending
			delete(c.pendingOrders, order.OrderID)
			p.cancelFunc()

			// Отправляем событие без StopOrderEvent
			if c.orderHandler != nil {
				c.orderHandler(p.order)
			}
		}
	})

	c.pendingOrders[order.OrderID] = pending
	c.pendingMu.Unlock()

	// Ждем завершения контекста (либо таймаут, либо отмена)
	go func() {
		<-ctx.Done()
	}()
}

// handleStopOrderEventMatching обрабатывает событие стоп-ордера и матчит с order
func (c *Client) handleStopOrderEventMatching(stop StopOrderEvent) {
	c.logger.Debug("🛑 Received stop order event",
		slog.String("orderId", stop.OrderID),
		slog.String("symbol", stop.Symbol),
		slog.Float64("stopLossPrice", stop.StopLossPrice))

	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	// Ищем соответствующий pending order
	if pending, exists := c.pendingOrders[stop.OrderID]; exists {
		c.logger.Debug("✅ Matched stop order with pending order",
			slog.String("orderId", stop.OrderID))

		// Останавливаем таймер
		if pending.timer != nil {
			pending.timer.Stop()
		}

		pending.cancelFunc()

		// Удаляем из pending
		delete(c.pendingOrders, stop.OrderID)

		// Добавляем StopOrderEvent к OrderEvent
		pending.order.StopOrderEvent = &stop

		// Отправляем составное событие
		if c.orderHandler != nil {
			c.orderHandler(pending.order)
		}
	} else {
		// Если нет соответствующего order (пришел раньше или отдельно)
		c.logger.Debug("⚠️ Stop order received without matching pending order",
			slog.String("orderId", stop.OrderID))

		// Вызываем отдельный обработчик для stop order
		if c.stopOrderHandler != nil {
			c.stopOrderHandler(stop)
		}
	}
}

func (c *Client) sendPings() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			ping := Message{Method: "ping"}

			if err := c.conn.WriteJSON(ping); err != nil {
				c.logger.Error("WebSocket ping error", slog.Any("error", err))
				return
			}
		}
	}
}
