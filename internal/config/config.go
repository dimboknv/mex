package config

import (
	"log/slog"
	"os"
)

// Config содержит конфигурацию приложения
type Config struct {
	TelegramToken string
	DBPath        string
	DryRun        bool // Режим тестирования - только логирование, без реальных сделок
	JWTSecret     string
	APIURL        string

	// Webhook configuration
	WebhookURL  string // URL для webhook (e.g., https://tg.example.com/webhook)
	WebhookPath string // Path для webhook endpoint (e.g., /webhook)
	Address     string // Address для HTTP сервера (e.g., 0.0.0.0:8080)
}

// Load загружает конфигурацию из переменных окружения
func Load(logger *slog.Logger) *Config {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		logger.Error("❌ TELEGRAM_BOT_TOKEN not set")
		os.Exit(1)
	}

	// Проверяем DRY_RUN флаг (по умолчанию true для безопасности)
	dryRun := true
	if os.Getenv("DRY_RUN") == "false" {
		dryRun = false

		logger.Warn("⚠️  DRY_RUN disabled - REAL TRADES WILL BE EXECUTED!")
	} else {
		logger.Info("🔍 DRY_RUN enabled - only logging, no real trades")
	}

	// Webhook configuration
	webhookURL := os.Getenv("WEBHOOK_URL")
	webhookPath := os.Getenv("WEBHOOK_PATH")
	if webhookPath == "" {
		webhookPath = "/webhook"
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "default-secret-change-me-in-production" // В продакшене использовать настоящий секрет!

		logger.Warn("⚠️  JWT_SECRET not set, using default (insecure!)")
	}

	// API URL для frontend и mirror скрипта
	apiURL := os.Getenv("API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}

	address := os.Getenv("ADDRESS")
	if address == "" {
		address = "0.0.0.0:8080"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./mexc.db"
	}

	if webhookURL != "" {
		logger.Info("🔗 Webhook mode enabled", slog.String("url", webhookURL))
	} else {
		logger.Info("📡 Polling mode enabled")
	}

	return &Config{
		TelegramToken: token,
		DBPath:        dbPath,
		JWTSecret:     jwtSecret,
		APIURL:        apiURL,
		DryRun:        dryRun,
		WebhookURL:    webhookURL,
		WebhookPath:   webhookPath,
		Address:       address,
	}
}
