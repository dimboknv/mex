package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"tg_mexc/internal/api/copytrading"
	"tg_mexc/internal/api/middleware"
)

// SetModeRequest - запрос на установку режима copy trading
type SetModeRequest struct {
	Mode       copytrading.Mode `json:"mode"` // "off", "websocket", "mirror"
	IgnoreFees bool             `json:"ignore_fees,omitempty"`
}

// HandleSetMode устанавливает режим copy trading
func (h *Handler) HandleSetMode(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())
	username, _ := middleware.GetUsername(r.Context())

	var req SetModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Валидация режима
	switch req.Mode {
	case copytrading.ModeOff, copytrading.ModeWebSocket, copytrading.ModeMirror:
		// OK
	default:
		h.respondError(w, http.StatusBadRequest, "Invalid mode. Use: off, websocket, or mirror")
		return
	}

	opts := copytrading.ModeOptions{
		IgnoreFees: req.IgnoreFees,
	}

	if err := h.copyTradingSvc.SetMode(r.Context(), userID, username, req.Mode, opts); err != nil {
		h.respondError(w, http.StatusConflict, err.Error())
		return
	}

	// Возвращаем обновлённый статус
	status := h.copyTradingSvc.GetStatus(r.Context(), userID, username)

	h.respondSuccess(w, "Mode changed successfully", status)
}

// HandleGetStatus возвращает текущий статус copy trading
func (h *Handler) HandleGetStatus(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())
	username, _ := middleware.GetUsername(r.Context())

	status := h.copyTradingSvc.GetStatus(r.Context(), userID, username)

	h.respondSuccess(w, "", status)
}

// HandleGetTrades возвращает историю сделок
func (h *Handler) HandleGetTrades(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	limit, offset := parsePagination(r, 50, 100)

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

	limit, offset := parsePagination(r, 100, 500)

	logs, err := h.storage.GetLogs(userID, limit, offset)
	if err != nil {
		h.logger.Error("Failed to get logs", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to get logs")
		return
	}

	h.respondSuccess(w, "", logs)
}

func parsePagination(r *http.Request, defaultLimit, maxLimit int) (limit, offset int) {
	limit = defaultLimit
	offset = 0

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= maxLimit {
			limit = parsed
		}
	}

	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	return limit, offset
}
