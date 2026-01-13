package models

// Account представляет аккаунт пользователя на MEXC
type Account struct {
	ID        int
	ChatID    int64             // Telegram chat ID владельца
	Name      string            // Имя аккаунта
	Token     string            // uc_token из браузера
	UserID    string            // u_id из браузера
	DeviceID  string            // deviceId (fingerprint)
	Cookies   map[string]string // Все cookies
	UserAgent string            // User Agent браузера
	Proxy     string            // Прокси (опционально)
	IsMaster  bool              // Главный аккаунт для copy trading
	Disabled  bool              // Отключен из-за наличия комиссии
}

// BrowserData - данные из браузера
type BrowserData struct {
	UcToken    string            `json:"uc_token"`
	UID        string            `json:"u_id"`
	DeviceID   string            `json:"deviceId"`
	AllCookies map[string]string `json:"allCookies"`
	UserAgent  string            `json:"userAgent"`
	Timezone   string            `json:"timezone"`
}

// OpenPositionRequest - запрос на открытие позиции
type OpenPositionRequest struct {
	Symbol        string `json:"symbol"`
	Side          int    `json:"side"`
	OpenType      int    `json:"openType"`
	Type          string `json:"type"` // "5" для market order (СТРОКА!)
	Vol           int    `json:"vol"`
	Leverage      int    `json:"leverage"`
	MarketCeiling bool   `json:"marketCeiling"`
	StopLossPrice string `json:"stopLossPrice,omitempty"` // СТРОКА!
	LossTrend     string `json:"lossTrend,omitempty"`     // "1" (СТРОКА!)
	PriceProtect  string `json:"priceProtect"`            // "0" (СТРОКА!)

	// Технические поля для шифрования
	P0     string `json:"p0,omitempty"`
	K0     string `json:"k0,omitempty"`
	Chash  string `json:"chash,omitempty"`
	Mtoken string `json:"mtoken,omitempty"`
	Ts     int64  `json:"ts,omitempty"`
	Mhash  string `json:"mhash,omitempty"`
}

// ClosePositionRequest - запрос на закрытие позиции
type ClosePositionRequest struct {
	Symbol       string `json:"symbol"`
	OpenType     int    `json:"openType"`
	PositionID   int64  `json:"positionId"`
	Leverage     int    `json:"leverage"`
	Type         int    `json:"type"` // 5 для market order (ЧИСЛО!)
	Vol          int    `json:"vol"`
	Side         int    `json:"side"`
	PriceProtect string `json:"priceProtect"` // "0" (СТРОКА!)

	// Технические поля для шифрования
	P0     string `json:"p0,omitempty"`
	K0     string `json:"k0,omitempty"`
	Chash  string `json:"chash,omitempty"`
	Mtoken string `json:"mtoken,omitempty"`
	Ts     int64  `json:"ts,omitempty"`
	Mhash  string `json:"mhash,omitempty"`
}

// OrderResponse - ответ на создание ордера
type OrderResponse struct {
	Success bool   `json:"success"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		OrderID string `json:"orderId"`
		Ts      int64  `json:"ts"`
	} `json:"data"`
}

// Position - позиция
type Position struct {
	PositionID   int64   `json:"positionId"`
	Symbol       string  `json:"symbol"`
	PositionType int     `json:"positionType"`
	HoldVol      float64 `json:"holdVol"`
	HoldAvgPrice float64 `json:"holdAvgPrice"`
	Leverage     int     `json:"leverage"`
}

// Balance - баланс
type Balance struct {
	Currency         string  `json:"currency"`
	AvailableBalance float64 `json:"availableBalance"`
	Equity           float64 `json:"equity"`
}

// LeverageInfo - информация о leverage
type LeverageInfo struct {
	Level           int     `json:"level"`
	MaxVol          float64 `json:"maxVol"`
	MMR             float64 `json:"mmr"`
	IMR             float64 `json:"imr"`
	PositionType    int     `json:"positionType"` // 1: long, 2: short
	OpenType        int     `json:"openType"`     // 1: isolated, 2: cross
	Leverage        int     `json:"leverage"`
	LimitBySys      bool    `json:"limitBySys"`
	CurrentMMR      float64 `json:"currentMmr"`
	MaxLeverageView int     `json:"maxLeverageView"`
}

// StopLossRequest - запрос на установку SL/TP
type StopLossRequest struct {
	Symbol          string  `json:"symbol"`
	StopLossPrice   float64 `json:"stopLossPrice,omitempty"`
	TakeProfitPrice float64 `json:"takeProfitPrice,omitempty"`
}

// StopOrderCancelItem - элемент для отмены стоп-ордера
type StopOrderCancelItem struct {
	StopPlanOrderID int64 `json:"stopPlanOrderId"`
}

// StopOrder - открытый стоп-ордер
type StopOrder struct {
	// ID              int64   `json:"id"`
	// Symbol          string  `json:"symbol"`
	// PositionID      string  `json:"positionId"`
	// StopLossPrice   float64 `json:"stopLossPrice"`
	// TakeProfitPrice float64 `json:"takeProfitPrice"`
	// State           int     `json:"state"`

	Id                         int     `json:"id"`
	OrderId                    string  `json:"orderId"`
	Symbol                     string  `json:"symbol"`
	PositionId                 string  `json:"positionId"`
	LossTrend                  int     `json:"lossTrend"`
	ProfitTrend                int     `json:"profitTrend"`
	StopLossPrice              float64 `json:"stopLossPrice"`
	State                      int     `json:"state"`
	TriggerSide                int     `json:"triggerSide"`
	PositionType               int     `json:"positionType"`
	Vol                        int     `json:"vol"`
	RealityVol                 int     `json:"realityVol"`
	ErrorCode                  int     `json:"errorCode"`
	Version                    int     `json:"version"`
	IsFinished                 int     `json:"isFinished"`
	PriceProtect               int     `json:"priceProtect"`
	ProfitLossVolType          string  `json:"profitLossVolType"`
	StopLossVol                int     `json:"stopLossVol"`
	CreateTime                 int64   `json:"createTime"`
	UpdateTime                 int64   `json:"updateTime"`
	VolType                    int     `json:"volType"`
	TakeProfitReverse          int     `json:"takeProfitReverse"`
	StopLossReverse            int     `json:"stopLossReverse"`
	CloseTryTimes              int     `json:"closeTryTimes"`
	ReverseTryTimes            int     `json:"reverseTryTimes"`
	ReverseErrorCode           int     `json:"reverseErrorCode"`
	StopLossType               int     `json:"stopLossType"`
	ProfitLOSSVOLTYPESAME      string  `json:"profit_LOSS_VOL_TYPE_SAME"`
	ProfitLOSSVOLTYPEDIFFERENT string  `json:"profit_LOSS_VOL_TYPE_DIFFERENT"`
}

// ChangePlanPriceRequest - запрос на изменение цены stop loss
type ChangePlanPriceRequest struct {
	StopPlanOrderID   int     `json:"stopPlanOrderId"`
	LossTrend         int     `json:"lossTrend"`
	ProfitTrend       int     `json:"profitTrend"`
	StopLossReverse   int     `json:"stopLossReverse"`
	TakeProfitReverse int     `json:"takeProfitReverse"`
	StopLossPrice     float64 `json:"stopLossPrice,omitempty"`
}

// OpenOrder - открытый ордер
type OpenOrder struct {
	OrderID                  string  `json:"orderId"`
	Symbol                   string  `json:"symbol"`
	PositionID               int64   `json:"positionId"`
	Price                    float64 `json:"price"`
	PriceStr                 string  `json:"priceStr"`
	Vol                      float64 `json:"vol"`
	Leverage                 int     `json:"leverage"`
	Side                     int     `json:"side"`
	Category                 int     `json:"category"`
	OrderType                int     `json:"orderType"`
	DealAvgPrice             float64 `json:"dealAvgPrice"`
	DealAvgPriceStr          string  `json:"dealAvgPriceStr"`
	DealVol                  float64 `json:"dealVol"`
	OrderMargin              float64 `json:"orderMargin"`
	TakerFee                 float64 `json:"takerFee"`
	MakerFee                 float64 `json:"makerFee"`
	Profit                   float64 `json:"profit"`
	FeeCurrency              string  `json:"feeCurrency"`
	OpenType                 int     `json:"openType"`
	State                    int     `json:"state"`
	ExternalOid              string  `json:"externalOid"`
	ErrorCode                int     `json:"errorCode"`
	UsedMargin               float64 `json:"usedMargin"`
	CreateTime               int64   `json:"createTime"`
	UpdateTime               int64   `json:"updateTime"`
	PositionMode             int     `json:"positionMode"`
	ReduceOnly               bool    `json:"reduceOnly"`
	Version                  int     `json:"version"`
	ShowCancelReason         int     `json:"showCancelReason"`
	ShowProfitRateShare      int     `json:"showProfitRateShare"`
	BboTypeNum               int     `json:"bboTypeNum"`
	PnlRate                  float64 `json:"pnlRate"`
	OpenAvgPrice             float64 `json:"openAvgPrice"`
	ZeroSaveTotalFeeBinance  float64 `json:"zeroSaveTotalFeeBinance"`
	ZeroTradeTotalFeeBinance float64 `json:"zeroTradeTotalFeeBinance"`
	TotalFee                 float64 `json:"totalFee"`
}

// TieredFeeRate - конфигурация ступенчатой комиссии
type TieredFeeRate struct {
	TieredDealAmount        float64 `json:"tieredDealAmount"`
	TieredEffectiveDay      int     `json:"tieredEffectiveDay"`
	TieredAppointContract   bool    `json:"tieredAppointContract"`
	TieredExcludeContractId bool    `json:"tieredExcludeContractId"`
	TieredContractIds       string  `json:"tieredContractIds"`
	TieredExcludeZeroFee    bool    `json:"tieredExcludeZeroFee"`
}

// LeverageFeeRate - конфигурация комиссии для кредитного плеча
type LeverageFeeRate struct {
	// Add fields as needed when API response is known
}

// TieredFeeRateResponse - ответ на запрос тарифной комиссии
type TieredFeeRateResponse struct {
	OriginalMakerFee float64           `json:"originalMakerFee"`
	OriginalTakerFee float64           `json:"originalTakerFee"`
	JoinDiscount     bool              `json:"joinDiscount"`
	EnjoyDiscount    bool              `json:"enjoyDiscount"`
	JoinDeduct       bool              `json:"joinDeduct"`
	EnjoyDeduct      bool              `json:"enjoyDeduct"`
	RealMakerFee     float64           `json:"realMakerFee"`
	RealTakerFee     float64           `json:"realTakerFee"`
	DiscountRate     float64           `json:"discountRate"`
	DeductRate       float64           `json:"deductRate"`
	DealAmount       float64           `json:"dealAmount"`
	WalletBalance    float64           `json:"walletBalance"`
	InviterKyc       string            `json:"inviterKyc"`
	FeeRateMode      string            `json:"feeRateMode"`
	TieredFeeRates   []TieredFeeRate   `json:"tieredFeeRates"`
	LeverageFeeRates []LeverageFeeRate `json:"leverageFeeRates"`
	StatisticType    string            `json:"statisticType"`
	FixedStartTime   int64             `json:"fixedStartTime"`
	FixedEndTime     int64             `json:"fixedEndTime"`
}
