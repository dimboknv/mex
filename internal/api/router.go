package api

import (
	"io/fs"
	"net/http"

	"tg_mexc/internal/middleware"
	"tg_mexc/web"

	"github.com/gorilla/mux"
)

// SetupRouter настраивает роутинг для API
func (h *Handler) SetupRouter() *mux.Router {
	r := mux.NewRouter()

	// Применяем CORS middleware ко всем маршрутам
	r.Use(middleware.CORS)

	// Публичные маршруты (не требуют аутентификации)
	r.HandleFunc("/api/auth/login", h.HandleLogin).Methods("POST", "OPTIONS")
	r.HandleFunc("/api/auth/register", h.HandleRegister).Methods("POST", "OPTIONS")
	r.HandleFunc("/health", h.HandleHealth).Methods("GET")
	r.HandleFunc("/config.js", h.HandleConfigJS).Methods("GET")

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

	// Mirror receive endpoint (публичный, использует токен в header)
	r.HandleFunc("/api/mirror/receive", h.HandleMirrorReceive).Methods("POST", "OPTIONS")

	// Mirror API endpoints - перехват прямых MEXC API запросов
	// Эти маршруты обрабатывают запросы от browser mirror скрипта
	r.PathPrefix("/api/platform/futures/").HandlerFunc(h.HandleMirrorAPI).Methods("POST", "OPTIONS")

	// Статические файлы из embedded FS (должны быть в конце)
	staticFS, _ := fs.Sub(web.StaticFiles, ".")
	r.PathPrefix("/").Handler(http.FileServer(http.FS(staticFS)))

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
