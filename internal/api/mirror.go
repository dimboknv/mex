package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"tg_mexc/internal/api/middleware"
)

// HandleMirrorAPI обрабатывает прямые API запросы от browser mirror
func (h *Handler) HandleMirrorAPI(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("X-Mirror-Token")
	if token == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	userID, _, ok := h.copyTradingSvc.ValidateMirrorToken(token)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("Failed to read request body", slog.Any("error", err))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	path := r.URL.Path

	h.logger.Info("Mirror API request",
		slog.Int("user_id", userID),
		slog.String("path", path),
	)

	// Обрабатываем в горутине
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := h.copyTradingSvc.ProcessMirrorRequest(ctx, token, path, body); err != nil {
			h.logger.Error("Mirror request failed",
				slog.String("path", path),
				slog.Int("user_id", userID),
				slog.Any("error", err))
		}
	}()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success":true}`))
}

// HandleGetMirrorScript возвращает JS скрипт для mirror режима
func (h *Handler) HandleGetMirrorScript(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())
	username, _ := middleware.GetUsername(r.Context())

	script := h.copyTradingSvc.GetMirrorScript(userID, username)

	h.respondSuccess(w, "", map[string]string{
		"script":     script,
		"mirror_url": h.apiURL,
	})
}
