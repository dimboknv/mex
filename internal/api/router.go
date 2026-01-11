package api

import (
	"net/http"
	"tg_mexc/internal/middleware"

	"github.com/gorilla/mux"
)

// SetupRouter настраивает роутинг для API
func (h *Handler) SetupRouter(webDir string) *mux.Router {
	r := mux.NewRouter()

	// Применяем CORS middleware ко всем маршрутам
	r.Use(middleware.CORS)

	// Публичные маршруты (не требуют аутентификации)
	r.HandleFunc("/api/auth/login", h.HandleLogin).Methods("POST", "OPTIONS")
	r.HandleFunc("/api/auth/register", h.HandleRegister).Methods("POST", "OPTIONS")
	r.HandleFunc("/health", h.HandleHealth).Methods("GET")

	// Защищенные маршруты (требуют аутентификации)
	api := r.PathPrefix("/api").Subrouter()
	api.Use(middleware.AuthMiddleware(h.authService))

	// Accounts
	api.HandleFunc("/accounts", h.HandleGetAccounts).Methods("GET")
	api.HandleFunc("/accounts/details", h.HandleGetAccountsWithDetails).Methods("GET")
	api.HandleFunc("/accounts", h.HandleAddAccount).Methods("POST")
	api.HandleFunc("/accounts/{id:[0-9]+}", h.HandleDeleteAccount).Methods("DELETE")
	api.HandleFunc("/accounts/{id:[0-9]+}/master", h.HandleSetMaster).Methods("PUT")
	api.HandleFunc("/accounts/{id:[0-9]+}/disabled", h.HandleToggleDisabled).Methods("PUT")
	api.HandleFunc("/accounts/script", h.HandleGetScript).Methods("GET")

	// Copy Trading
	api.HandleFunc("/copy-trading/start", h.HandleStartCopyTrading).Methods("POST")
	api.HandleFunc("/copy-trading/stop", h.HandleStopCopyTrading).Methods("POST")
	api.HandleFunc("/copy-trading/status", h.HandleGetCopyTradingStatus).Methods("GET")

	// Trades History
	api.HandleFunc("/trades", h.HandleGetTrades).Methods("GET")

	// Activity Logs
	api.HandleFunc("/logs", h.HandleGetLogs).Methods("GET")

	// Mirror (Browser Copy Trading)
	api.HandleFunc("/mirror/script", h.HandleGetMirrorScript).Methods("GET")

	// Mirror receive endpoint (публичный, использует токен в query)
	r.HandleFunc("/api/mirror/receive", h.HandleMirrorReceive).Methods("POST", "OPTIONS")

	// Статические файлы (должны быть в конце)
	r.PathPrefix("/").Handler(http.FileServer(http.Dir(webDir)))

	return r
}

// HandleHealth возвращает статус здоровья сервиса
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	h.respondSuccess(w, "OK", map[string]string{
		"status": "healthy",
	})
}
