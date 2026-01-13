package middleware

import (
	"net/http"
	"strings"
)

// CORS middleware для разрешения запросов с фронтенда
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Для mirror endpoints разрешаем все заголовки
		if strings.HasPrefix(r.URL.Path, "/api/platform/") || strings.HasPrefix(r.URL.Path, "/api/mirror/") {
			// Разрешаем все заголовки которые пришли в запросе
			requestedHeaders := r.Header.Get("Access-Control-Request-Headers")
			if requestedHeaders != "" {
				w.Header().Set("Access-Control-Allow-Headers", requestedHeaders)
			} else {
				w.Header().Set("Access-Control-Allow-Headers", "*")
			}
		} else {
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		// Обработка preflight запросов
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
