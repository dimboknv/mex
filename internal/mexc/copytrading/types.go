package copytrading

// OpenPositionRequest - запрос на открытие позиции
type OpenPositionRequest struct {
	Symbol        string
	Side          int // 1=open long, 3=open short
	Volume        float64
	Leverage      int
	StopLossPrice float64 // optional, 0 если не нужен
}

// ClosePositionRequest - запрос на закрытие позиции
type ClosePositionRequest struct {
	Symbol     string
	Side       int     // 2=close short, 4=close long
	Volume     float64 // объём для закрытия (0 = закрыть всё)
	PositionID int64   // ID позиции (опционально)
}

// PlacePlanOrderRequest - запрос на установку SL/TP
type PlacePlanOrderRequest struct {
	Symbol          string
	StopLossPrice   float64
	TakeProfitPrice float64
	LossTrend       int
	ProfitTrend     int
}

// ChangePlanPriceRequest - запрос на изменение SL/TP
type ChangePlanPriceRequest struct {
	StopPlanOrderID   int
	Symbol            string // Опционально: если передан, не нужен API lookup
	StopLossPrice     float64
	LossTrend         int
	ProfitTrend       int
	StopLossReverse   int
	TakeProfitReverse int
}

// ChangeLeverageRequest - запрос на изменение leverage
type ChangeLeverageRequest struct {
	Symbol       string
	Leverage     int
	OpenType     int // 1=isolated
	PositionType int // 1=long, 2=short
}

// CancelStopOrderRequest - запрос на отмену стоп-ордера
type CancelStopOrderRequest struct {
	Symbol string
}

// AccountResult - результат выполнения операции на одном аккаунте
type AccountResult struct {
	AccountID   int
	AccountName string
	Success     bool
	Error       string
	OrderID     string
	LatencyMs   int64
}

// ExecutionResult - результат выполнения операции на всех slave аккаунтах
type ExecutionResult struct {
	TotalCount   int
	SuccessCount int
	FailedCount  int
	Results      []AccountResult
}

// IsFullSuccess возвращает true если все операции успешны
func (r *ExecutionResult) IsFullSuccess() bool {
	return r.FailedCount == 0
}

// IsPartialSuccess возвращает true если есть и успешные и неуспешные операции
func (r *ExecutionResult) IsPartialSuccess() bool {
	return r.SuccessCount > 0 && r.FailedCount > 0
}

// IsFullFailure возвращает true если все операции неуспешны
func (r *ExecutionResult) IsFullFailure() bool {
	return r.SuccessCount == 0 && r.TotalCount > 0
}
