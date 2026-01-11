package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"tg_mexc/config"
	"tg_mexc/handlers"
	"tg_mexc/services/copytrading"
	"tg_mexc/services/telegram"
	"tg_mexc/storage"
	"time"

	"github.com/lmittmann/tint"
)

func main() {
	// Конфигурация slog для вывода в файл и stdout
	logFile, err := os.OpenFile("bot_browser.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
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

	logger.Info("=== MEXC Copy Trading Bot (Browser Auth) ===")

	// Загрузка конфигурации
	cfg := config.Load(logger)

	// Инициализация хранилища
	store, err := storage.New(cfg.DBPath, logger)
	if err != nil {
		logger.Error("Failed to initialize storage", slog.Any("error", err))
		os.Exit(1)
	}
	defer store.Close()

	// Инициализация Telegram сервиса
	tgService, err := telegram.New(cfg.TelegramToken, logger)
	if err != nil {
		logger.Error("Failed to initialize Telegram service", slog.Any("error", err))
		os.Exit(1)
	}

	// Инициализация Copy Trading сервиса
	copyTradingService := copytrading.New(store, logger, cfg.DryRun)

	// Создание обработчика
	handler := handlers.New(store, tgService, copyTradingService, logger)

	// Запуск бота
	logger.Info("🚀 Starting bot...")
	logger.Info("📡 Listening for commands...")

	updates := tgService.GetUpdatesChan()

	for update := range updates {
		// Обработка каждого обновления в отдельной горутине
		go handler.HandleUpdate(update)
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
