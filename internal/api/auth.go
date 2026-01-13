package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	Username     string `json:"username"`
	UserID       int    `json:"user_id"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type RefreshResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// HandleLogin обрабатывает вход пользователя
func (h *Handler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Валидация
	if req.Username == "" || req.Password == "" {
		h.respondError(w, http.StatusBadRequest, "Username and password are required")
		return
	}

	// Получаем пользователя из БД
	user, err := h.storage.GetUserByUsername(req.Username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.respondError(w, http.StatusUnauthorized, "Invalid credentials")
			return
		}

		h.logger.Error("Failed to get user", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Internal server error")

		return
	}

	// Проверяем пароль
	if err := h.authService.VerifyPassword(user.PasswordHash, req.Password); err != nil {
		h.respondError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	// Генерируем JWT токен
	token, err := h.authService.GenerateToken(user.ID, user.Username)
	if err != nil {
		h.logger.Error("Failed to generate token", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Internal server error")

		return
	}

	// Генерируем refresh token
	refreshToken, err := h.authService.GenerateRefreshToken()
	if err != nil {
		h.logger.Error("Failed to generate refresh token", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Internal server error")

		return
	}

	// Сохраняем refresh token в БД
	expiresAt := time.Now().Add(h.authService.RefreshTokenTTL())
	if err := h.storage.SaveRefreshToken(user.ID, refreshToken, expiresAt); err != nil {
		h.logger.Error("Failed to save refresh token", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Internal server error")

		return
	}

	h.respondSuccess(w, "Login successful", LoginResponse{
		Token:        token,
		RefreshToken: refreshToken,
		Username:     user.Username,
		UserID:       user.ID,
	})
}

// HandleRegister обрабатывает регистрацию нового пользователя
func (h *Handler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Валидация
	if req.Username == "" || req.Password == "" {
		h.respondError(w, http.StatusBadRequest, "Username and password are required")
		return
	}

	if len(req.Password) < 6 {
		h.respondError(w, http.StatusBadRequest, "Password must be at least 6 characters")
		return
	}

	// Хешируем пароль
	passwordHash, err := h.authService.HashPassword(req.Password)
	if err != nil {
		h.logger.Error("Failed to hash password", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Internal server error")

		return
	}

	// Создаем пользователя
	user, err := h.storage.CreateUser(req.Username, passwordHash)
	if err != nil {
		// Проверяем на дублирование username
		if err.Error() == "UNIQUE constraint failed: users.username" {
			h.respondError(w, http.StatusConflict, "Username already exists")
			return
		}

		h.logger.Error("Failed to create user", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Internal server error")

		return
	}

	// Генерируем JWT токен
	token, err := h.authService.GenerateToken(user.ID, user.Username)
	if err != nil {
		h.logger.Error("Failed to generate token", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Internal server error")

		return
	}

	// Генерируем refresh token
	refreshToken, err := h.authService.GenerateRefreshToken()
	if err != nil {
		h.logger.Error("Failed to generate refresh token", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Internal server error")

		return
	}

	// Сохраняем refresh token в БД
	expiresAt := time.Now().Add(h.authService.RefreshTokenTTL())
	if err := h.storage.SaveRefreshToken(user.ID, refreshToken, expiresAt); err != nil {
		h.logger.Error("Failed to save refresh token", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Internal server error")

		return
	}

	h.respondSuccess(w, "Registration successful", LoginResponse{
		Token:        token,
		RefreshToken: refreshToken,
		Username:     user.Username,
		UserID:       user.ID,
	})
}

// HandleRefresh обновляет access token используя refresh token
func (h *Handler) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.RefreshToken == "" {
		h.respondError(w, http.StatusBadRequest, "Refresh token is required")
		return
	}

	// Проверяем refresh token в БД
	userID, err := h.storage.GetRefreshToken(req.RefreshToken)
	if err != nil {
		h.respondError(w, http.StatusUnauthorized, "Invalid refresh token")
		return
	}

	// Получаем пользователя
	user, err := h.storage.GetUserByID(userID)
	if err != nil {
		h.logger.Error("Failed to get user", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Удаляем старый refresh token
	h.storage.DeleteRefreshToken(req.RefreshToken)

	// Генерируем новый access token
	token, err := h.authService.GenerateToken(user.ID, user.Username)
	if err != nil {
		h.logger.Error("Failed to generate token", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Генерируем новый refresh token
	refreshToken, err := h.authService.GenerateRefreshToken()
	if err != nil {
		h.logger.Error("Failed to generate refresh token", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Сохраняем новый refresh token
	expiresAt := time.Now().Add(h.authService.RefreshTokenTTL())
	if err := h.storage.SaveRefreshToken(user.ID, refreshToken, expiresAt); err != nil {
		h.logger.Error("Failed to save refresh token", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	h.respondSuccess(w, "Token refreshed", RefreshResponse{
		Token:        token,
		RefreshToken: refreshToken,
	})
}

// HandleLogout инвалидирует refresh token
func (h *Handler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.RefreshToken != "" {
		h.storage.DeleteRefreshToken(req.RefreshToken)
	}

	h.respondSuccess(w, "Logged out successfully", nil)
}
