package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tg_mexc/internal/config"
	"tg_mexc/internal/mexc/copytrading"
	"tg_mexc/internal/storage"
	"tg_mexc/internal/telegram"
	telegramcopytrading "tg_mexc/internal/telegram/copytrading"
	"tg_mexc/internal/telegram/handlers"

	"github.com/lmittmann/tint"
)

func main() {
	fileHandler := tint.NewHandler(os.Stdout, &tint.Options{
		Level:      slog.LevelDebug,
		TimeFormat: time.Kitchen, // "3:04PM"
		AddSource:  false,
		NoColor:    false,
	})

	// Мультиплексируем логи в оба handler'а
	logger := slog.New(&multiHandler{
		handlers: []slog.Handler{fileHandler},
	})

	logger.Info("=== MEXC Copy Trading Bot (Browser Auth) ===")

	// Загрузка конфигурации
	cfg := config.Load(logger)

	// Инициализация хранилища (используем WebStorage для единой базы с web-app)
	webStorage, err := storage.NewWeb(cfg.DBPath, logger)
	if err != nil {
		logger.Error("Failed to initialize storage", slog.Any("error", err))
		os.Exit(1)
	}
	defer webStorage.Close()

	// Инициализация Telegram сервиса
	tgService, err := telegram.New(cfg.TelegramToken, logger)
	if err != nil {
		logger.Error("Failed to initialize Telegram service", slog.Any("error", err))
		os.Exit(1)
	}

	// Инициализация Copy Trading
	engine := copytrading.NewEngine(webStorage, webStorage, webStorage, webStorage, logger, cfg.DryRun)
	manager := copytrading.NewManager(engine, cfg.DryRun, logger)
	copyTradingSvc := telegramcopytrading.New(manager, webStorage, logger)

	// Создание обработчика
	handler := handlers.New(webStorage, tgService, copyTradingSvc, logger)

	// Запуск бота
	logger.Info("🚀 Starting bot...")

	// Выбор режима работы: webhook или polling
	if cfg.WebhookURL != "" {
		// Webhook mode
		webhookFullURL := cfg.WebhookURL + cfg.WebhookPath
		if err := tgService.SetWebhook(webhookFullURL); err != nil {
			logger.Error("Failed to set webhook", slog.Any("error", err))
			os.Exit(1)
		}

		// Создаем HTTP сервер для webhook
		mux := http.NewServeMux()
		mux.Handle(cfg.WebhookPath, tgService.ListenForWebhook(cfg.WebhookPath))

		// Health check endpoint
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		})

		srv := &http.Server{
			Addr:         cfg.Address,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		}

		// Запускаем HTTP сервер в горутине
		go func() {
			logger.Info("📡 Webhook server starting...", slog.String("address", cfg.Address))

			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("Webhook server failed", slog.Any("error", err))
				os.Exit(1)
			}
		}()

		// Обработка обновлений из webhook
		updates := tgService.GetWebhookUpdatesChan()
		go func() {
			for update := range updates {
				go handler.HandleUpdate(update)
			}
		}()

		// Graceful shutdown
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit

		logger.Info("🛑 Shutting down bot...")

		// Останавливаем copy trading
		copyTradingSvc.StopAll()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("Server forced to shutdown", slog.Any("error", err))
		}

		logger.Info("✅ Bot stopped")
	} else {
		// Polling mode (для локальной разработки)
		logger.Info("📡 Listening for commands (polling mode)...")

		updates := tgService.GetUpdatesChan()

		// Graceful shutdown для polling mode
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-quit
			logger.Info("🛑 Shutting down bot...")
			copyTradingSvc.StopAll()
			tgService.GetBot().StopReceivingUpdates()
		}()

		for update := range updates {
			go handler.HandleUpdate(update)
		}

		logger.Info("✅ Bot stopped")
	}
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
