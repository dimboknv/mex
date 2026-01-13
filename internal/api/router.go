package api

import (
	"net/http"

	middleware2 "tg_mexc/internal/api/middleware"
	"tg_mexc/internal/api/web"

	"github.com/gorilla/mux"
)

// SetupRouter настраивает роутинг для API
func (h *Handler) SetupRouter() *mux.Router {
	r := mux.NewRouter()

	// Применяем CORS middleware ко всем маршрутам
	r.Use(middleware2.CORS)

	// Публичные маршруты (не требуют аутентификации)
	r.HandleFunc("/api/auth/login", h.HandleLogin).Methods("POST", "OPTIONS")
	r.HandleFunc("/api/auth/register", h.HandleRegister).Methods("POST", "OPTIONS")
	r.HandleFunc("/api/auth/refresh", h.HandleRefresh).Methods("POST", "OPTIONS")
	r.HandleFunc("/api/auth/logout", h.HandleLogout).Methods("POST", "OPTIONS")
	r.HandleFunc("/health", h.HandleHealth).Methods("GET")
	r.HandleFunc("/config.js", h.HandleConfigJS).Methods("GET")

	// Защищенные маршруты (требуют аутентификации)
	api := r.PathPrefix("/api").Subrouter()
	api.Use(middleware2.AuthMiddleware(h.authService))

	// Accounts
	api.HandleFunc("/accounts", h.HandleGetAccounts).Methods("GET")
	api.HandleFunc("/accounts/details", h.HandleGetAccountsWithDetails).Methods("GET")
	api.HandleFunc("/accounts", h.HandleAddAccount).Methods("POST")
	api.HandleFunc("/accounts/{id:[0-9]+}", h.HandleDeleteAccount).Methods("DELETE")
	api.HandleFunc("/accounts/{id:[0-9]+}/master", h.HandleSetMaster).Methods("PUT")
	api.HandleFunc("/accounts/{id:[0-9]+}/disabled", h.HandleToggleDisabled).Methods("PUT")
	api.HandleFunc("/accounts/script", h.HandleGetScript).Methods("GET")

	// Copy Trading - единый API
	api.HandleFunc("/copy-trading/mode", h.HandleSetMode).Methods("POST")
	api.HandleFunc("/copy-trading/status", h.HandleGetStatus).Methods("GET")
	api.HandleFunc("/copy-trading/script", h.HandleGetMirrorScript).Methods("GET")

	// Trades History
	api.HandleFunc("/trades", h.HandleGetTrades).Methods("GET")
	api.HandleFunc("/trades/feed", h.HandleGetTradesFeed).Methods("GET")

	// Account trades history
	api.HandleFunc("/accounts/{id:[0-9]+}/trades", h.HandleGetAccountTrades).Methods("GET")

	// Activity Logs
	api.HandleFunc("/logs", h.HandleGetLogs).Methods("GET")

	// Mirror API endpoints - перехват MEXC API запросов
	r.PathPrefix("/api/platform/futures/").HandlerFunc(h.HandleMirrorAPI).Methods("POST", "OPTIONS")

	// Статические файлы (должны быть в конце)
	fileServer := http.FileServer(http.FS(web.StaticFiles))
	r.PathPrefix("/").Handler(fileServer)

	return r
}

// HandleHealth возвращает статус здоровья сервиса
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	h.respondSuccess(w, "OK", map[string]string{
		"status": "healthy",
	})
}

// HandleConfigJS возвращает JavaScript конфигурацию для frontend
func (h *Handler) HandleConfigJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "no-cache")

	js := `window.APP_CONFIG = {
    API_URL: "` + h.apiURL + `"
};`

	w.Write([]byte(js))
}
