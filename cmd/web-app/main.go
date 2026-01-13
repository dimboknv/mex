package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tg_mexc/internal/api"
	"tg_mexc/internal/api/auth"
	apicopytrading "tg_mexc/internal/api/copytrading"
	"tg_mexc/internal/config"
	"tg_mexc/internal/mexc/copytrading"
	"tg_mexc/internal/storage"

	"github.com/lmittmann/tint"
)

func main() {
	// Pretty handler для stdout с цветами
	prettyHandler := tint.NewHandler(os.Stdout, &tint.Options{
		Level:      slog.LevelDebug,
		TimeFormat: time.Kitchen, // "3:04PM"
		AddSource:  false,
		NoColor:    false,
	})

	// Мультиплексируем логи в оба handler'а
	logger := slog.New(&multiHandler{
		handlers: []slog.Handler{prettyHandler},
	})

	cfg := config.Load(logger)

	// Инициализация БД
	webStorage, err := storage.NewWeb(cfg.DBPath, logger)
	if err != nil {
		logger.Error("Failed to initialize storage", slog.Any("error", err))
		os.Exit(1)
	}
	defer webStorage.Close()

	// Инициализация auth сервиса
	authService := auth.NewService(cfg.JWTSecret, 24*time.Hour) // Токен действителен 24 часа

	// Инициализация copy trading сервисов
	engine := copytrading.NewEngine(webStorage, webStorage, webStorage, webStorage, logger, cfg.DryRun)
	manager := copytrading.NewManager(engine, cfg.DryRun, logger)

	// Создаём главный сервис copy trading
	copyTradingSvc := apicopytrading.NewService(manager, webStorage, cfg.APIURL, logger)

	// Инициализация API handler
	apiHandler := api.New(webStorage, authService, copyTradingSvc, cfg.APIURL, logger)

	// Настройка роутинга (статика встроена через go:embed)
	router := apiHandler.SetupRouter()

	// HTTP сервер
	srv := &http.Server{
		Addr:         cfg.Address,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Запускаем сервер в горутине
	go func() {
		logger.Info("🚀 Server starting...", slog.String("address", cfg.Address))
		logger.Info(fmt.Sprintf("📡 API available at %s", cfg.APIURL))

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

	// Останавливаем все активные сессии copy trading
	copyTradingSvc.StopAll()

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
