package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"tg_mexc/internal/middleware"
)

type StartCopyTradingRequest struct {
	IgnoreFees bool `json:"ignore_fees"`
}

type CopyTradingStatus struct {
	Active           bool   `json:"active"`
	MasterAccountID  int    `json:"master_account_id,omitempty"`
	MasterName       string `json:"master_name,omitempty"`
	ActiveSlaveCount int    `json:"active_slave_count"`
	IgnoreFees       bool   `json:"ignore_fees"`
}

// HandleStartCopyTrading запускает copy trading
func (h *Handler) HandleStartCopyTrading(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	var req StartCopyTradingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Проверяем наличие мастер аккаунта
	master, err := h.storage.GetMasterAccount(userID)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Master account not set")
		return
	}

	// Получаем slave аккаунты
	slaves, err := h.storage.GetSlaveAccounts(userID, req.IgnoreFees)
	if err != nil {
		h.logger.Error("Failed to get slave accounts", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to get slave accounts")

		return
	}

	if len(slaves) == 0 {
		h.respondError(w, http.StatusBadRequest, "No active slave accounts")
		return
	}

	// TODO: Запускаем copy trading через сервис
	// Пока что это заглушка, так как copy trading сервис работает с chatID
	// Нужно будет адаптировать его для работы с userID

	h.respondSuccess(w, "Copy trading started successfully", map[string]any{
		"master": master.Name,
		"slaves": len(slaves),
	})
}

// HandleStopCopyTrading останавливает copy trading
func (h *Handler) HandleStopCopyTrading(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	// TODO: Останавливаем copy trading через сервис
	_ = userID

	h.respondSuccess(w, "Copy trading stopped successfully", nil)
}

// HandleGetCopyTradingStatus возвращает статус copy trading
func (h *Handler) HandleGetCopyTradingStatus(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	// Получаем мастер аккаунт
	master, err := h.storage.GetMasterAccount(userID)

	status := CopyTradingStatus{
		Active: false,
	}

	if err == nil {
		status.MasterAccountID = master.ID
		status.MasterName = master.Name

		// Получаем активные slave аккаунты
		slaves, _ := h.storage.GetSlaveAccounts(userID, false)
		status.ActiveSlaveCount = len(slaves)

		// TODO: Проверяем действительно ли copy trading активен через сервис
		status.Active = false // h.copyTrading.IsActive(userID)
	}

	h.respondSuccess(w, "", status)
}

// HandleGetTrades возвращает историю сделок
func (h *Handler) HandleGetTrades(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	// Парсим параметры пагинации
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 50 // по умолчанию
	offset := 0

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	trades, err := h.storage.GetTrades(userID, limit, offset)
	if err != nil {
		h.logger.Error("Failed to get trades", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to get trades")

		return
	}

	h.respondSuccess(w, "", trades)
}

// HandleGetLogs возвращает логи активности
func (h *Handler) HandleGetLogs(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	// Парсим параметры пагинации
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 100 // по умолчанию
	offset := 0

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 500 {
			limit = l
		}
	}

	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	logs, err := h.storage.GetLogs(userID, limit, offset)
	if err != nil {
		h.logger.Error("Failed to get logs", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to get logs")

		return
	}

	h.respondSuccess(w, "", logs)
}
