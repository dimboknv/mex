package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"tg_mexc/internal/auth"
	"tg_mexc/pkg/services/copytrading"
	"tg_mexc/pkg/storage"
)

// Handler обрабатывает API запросы
type Handler struct {
	storage        *storage.WebStorage
	authService    *auth.Service
	copyTradingWeb *copytrading.WebService
	mirrorManager  *MirrorManager
	mirrorURL      string
	logger         *slog.Logger
}

// New создает новый API handler
func New(storage *storage.WebStorage, authService *auth.Service, copyTradingWeb *copytrading.WebService, mirrorURL string, logger *slog.Logger) *Handler {
	return &Handler{
		storage:        storage,
		authService:    authService,
		copyTradingWeb: copyTradingWeb,
		mirrorManager:  NewMirrorManager(logger),
		mirrorURL:      mirrorURL,
		logger:         logger,
	}
}

// Helper функции для JSON ответов

type ErrorResponse struct {
	Error string `json:"error"`
}

type SuccessResponse struct {
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

func (h *Handler) respondJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) respondError(w http.ResponseWriter, statusCode int, message string) {
	h.respondJSON(w, statusCode, ErrorResponse{Error: message})
}

func (h *Handler) respondSuccess(w http.ResponseWriter, message string, data any) {
	h.respondJSON(w, http.StatusOK, SuccessResponse{
		Message: message,
		Data:    data,
	})
}
