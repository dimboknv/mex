package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"tg_mexc/internal/api"
	"tg_mexc/internal/auth"
	"tg_mexc/pkg/services/copytrading"
	"tg_mexc/pkg/storage"
	"time"

	"github.com/lmittmann/tint"
)

func main() {
	// Конфигурация slog для вывода в файл и stdout
	logFile, err := os.OpenFile("web_app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		log.Fatal("Failed to open log file:", err)
	}
	defer logFile.Close()

	// Pretty handler для stdout с цветами
	prettyHandler := tint.NewHandler(os.Stdout, &tint.Options{
		Level:      slog.LevelDebug,
		TimeFormat: time.Kitchen, // "3:04PM"
		AddSource:  false,
		NoColor:    false,
	})

	// Обычный текстовый handler для файла
	fileHandler := slog.NewTextHandler(logFile, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	// Мультиплексируем логи в оба handler'а
	logger := slog.New(&multiHandler{
		handlers: []slog.Handler{prettyHandler, fileHandler},
	})

	logger.Info("=== MEXC Copy Trading Web App ===")

	// Загрузка конфигурации из env
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "default-secret-change-me-in-production" // В продакшене использовать настоящий секрет!

		logger.Warn("⚠️  JWT_SECRET not set, using default (insecure!)")
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./web_app.db"
	}

	webDir := os.Getenv("WEB_DIR")
	if webDir == "" {
		webDir = "../../web/" // По умолчанию из cmd/web-app
	}

	mirrorURL := os.Getenv("MIRROR_URL")
	if mirrorURL == "" {
		mirrorURL = "http://localhost:" + port // По умолчанию используем текущий сервер
	}

	// Проверяем DRY_RUN флаг
	dryRun := true
	if os.Getenv("DRY_RUN") == "false" {
		dryRun = false

		logger.Warn("⚠️  DRY_RUN disabled - REAL TRADES WILL BE EXECUTED!")
	} else {
		logger.Info("🔍 DRY_RUN enabled - only logging, no real trades")
	}

	// Инициализация БД
	webStorage, err := storage.NewWeb(dbPath, logger)
	if err != nil {
		logger.Error("Failed to initialize storage", slog.Any("error", err))
		os.Exit(1)
	}
	defer webStorage.Close()

	// Инициализация auth сервиса
	authService := auth.NewService(jwtSecret, 24*time.Hour) // Токен действителен 24 часа

	// Инициализация copy trading сервиса
	// ВАЖНО: Здесь используется пустой storage, так как copy trading сервис
	// ожидает старый storage. Нужно будет адаптировать его для работы с WebStorage
	copyTradingService := copytrading.New(nil, logger, dryRun)

	// Инициализация API handler
	apiHandler := api.New(webStorage, authService, copyTradingService, mirrorURL, logger)

	// Настройка роутинга
	router := apiHandler.SetupRouter(webDir)

	// HTTP сервер
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Запускаем сервер в горутине
	go func() {
		logger.Info("🚀 Server starting...", slog.String("port", port))
		logger.Info(fmt.Sprintf("📡 API available at http://localhost:%s/api", port))
		logger.Info(fmt.Sprintf("🏥 Health check at http://localhost:%s/health", port))

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server failed to start", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("🛑 Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", slog.Any("error", err))
	}

	logger.Info("✅ Server stopped")
}

// multiHandler отправляет логи в несколько handlers одновременно
type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}

	return false
}

func (m *multiHandler) Handle(ctx context.Context, record slog.Record) error {
	for _, h := range m.handlers {
		if err := h.Handle(ctx, record); err != nil {
			return err
		}
	}

	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}

	return &multiHandler{handlers: handlers}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}

	return &multiHandler{handlers: handlers}
}
