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
	DryRun           bool   `json:"dry_run"`
}

// HandleStartCopyTrading запускает copy trading
func (h *Handler) HandleStartCopyTrading(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	var req StartCopyTradingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	master, err := h.storage.GetMasterAccount(userID)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Master account not set")
		return
	}

	if err := h.wsCopyTradingManager.starSession(userID); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	slaves, err := h.storage.GetSlaveAccounts(userID, req.IgnoreFees)
	if err != nil {
		h.logger.Error("Failed to get slave accounts", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to get slave accounts")
		return
	}

	h.respondSuccess(w, "Copy trading started successfully", map[string]any{
		"master":  master.Name,
		"slaves":  len(slaves),
		"dry_run": h.manager.IsDryRun(),
	})
}

// HandleStopCopyTrading останавливает copy trading
func (h *Handler) HandleStopCopyTrading(w http.ResponseWriter, r *http.Request) {
	userID, _ := h.getUserFromContext(r)

	// Останавливаем WebSocket copy trading если активен
	if err := h.wsCopyTradingManager.stopSession(userID); err != nil {
		h.respondError(w, http.StatusConflict, err.Error())
		return
	}

	h.respondSuccess(w, "Copy trading stopped successfully", nil)
}

// HandleGetCopyTradingStatus возвращает статус copy trading
func (h *Handler) HandleGetCopyTradingStatus(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	// Получаем мастер аккаунт
	master, err := h.storage.GetMasterAccount(userID)

	status := CopyTradingStatus{
		Active: false,
		DryRun: h.manager.IsDryRun(),
	}

	if err == nil {
		status.MasterAccountID = master.ID
		status.MasterName = master.Name

		// Получаем активные slave аккаунты
		slaves, _ := h.storage.GetSlaveAccounts(userID, false)
		status.ActiveSlaveCount = len(slaves)

		// Проверяем активность copy trading через WebService
		status.Active = h.wsCopyTradingManager.isActive(userID)

		// Если активен, получаем дополнительные данные из сессии
		if status.Active {
			// _, slaveCount, ignoreFees, isDryRun := h.copyTradingWeb.GetStatus(userID)
			// status.ActiveSlaveCount = slaveCount
			// status.IgnoreFees = ignoreFees
			// status.DryRun = isDryRun
		}
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

// UnifiedCopyTradingStatus - объединенный статус copy trading
type UnifiedCopyTradingStatus struct {
	Mode             string `json:"mode"` // "off", "websocket", "mirror"
	MasterAccountID  int    `json:"master_account_id,omitempty"`
	MasterName       string `json:"master_name,omitempty"`
	ActiveSlaveCount int    `json:"active_slave_count"`
	IgnoreFees       bool   `json:"ignore_fees"`
	DryRun           bool   `json:"dry_run"`
	// Mirror-specific fields
	MirrorToken string `json:"mirror_token,omitempty"`
	MirrorURL   string `json:"mirror_url,omitempty"`
	// Script for mirror mode
	MirrorScript string `json:"mirror_script,omitempty"`
}

// HandleGetUnifiedStatus возвращает объединенный статус copy trading
func (h *Handler) HandleGetUnifiedStatus(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())
	username, _ := middleware.GetUsername(r.Context())

	status := UnifiedCopyTradingStatus{
		Mode:   "off",
		DryRun: h.manager.IsDryRun(),
	}

	// Получаем мастер аккаунт
	master, err := h.storage.GetMasterAccount(userID)
	if err == nil {
		status.MasterAccountID = master.ID
		status.MasterName = master.Name

		// Получаем активные slave аккаунты
		slaves, _ := h.storage.GetSlaveAccounts(userID, false)
		status.ActiveSlaveCount = len(slaves)
	}

	if _, err := h.manager.GetSession(userID, "websocket"); err == nil {
		status.Mode = "websocket"
	} else if _, err := h.manager.GetSession(userID, "mirror"); err == nil {
		status.Mode = "mirror"
		token := h.mirrorManager.GetTokenForUser(userID, username)
		status.MirrorToken = token
		status.MirrorURL = h.apiURL
		status.MirrorScript = GenerateMirrorScript(h.apiURL, token)
	}

	h.respondSuccess(w, "", status)
}
