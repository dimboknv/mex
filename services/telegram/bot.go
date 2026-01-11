package telegram

import (
	"log/slog"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Service управляет Telegram ботом
type Service struct {
	bot    *tgbotapi.BotAPI
	logger *slog.Logger
}

// New создает новый Telegram сервис
func New(token string, logger *slog.Logger) (*Service, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	logger.Info("✅ Bot authorized", slog.String("username", bot.Self.UserName))

	// Устанавливаем команды для меню
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "Начать работу"},
		{Command: "script", Description: "Получить JS скрипт для браузера"},
		{Command: "list", Description: "Список аккаунтов"},
		{Command: "balance", Description: "Баланс всех аккаунтов"},
		{Command: "fee_rates", Description: "Проверить комиссии всех аккаунтов"},
		{Command: "set_master", Description: "Установить главный аккаунт"},
		{Command: "start_copy", Description: "Запустить copy trading [ignore_fees]"},
		{Command: "stop_copy", Description: "Остановить copy trading"},
		{Command: "copy_status", Description: "Статус copy trading"},
		{Command: "open", Description: "Открыть на аккаунте"},
		{Command: "close", Description: "Закрыть на аккаунте"},
		{Command: "open_all", Description: "Открыть на всех аккаунтах"},
		{Command: "close_all", Description: "Закрыть на всех аккаунтах"},
		{Command: "positions", Description: "Показать открытые позиции"},
		{Command: "open_orders", Description: "Показать открытые ордера"},
		{Command: "open_stop_orders", Description: "Показать стоп-ордера"},
		{Command: "delete", Description: "Удалить аккаунт"},
		{Command: "help", Description: "Помощь"},
	}

	cfg := tgbotapi.NewSetMyCommands(commands...)
	_, err = bot.Request(cfg)
	if err != nil {
		logger.Error("Failed to set commands", slog.Any("error", err))
	} else {
		logger.Info("✅ Bot commands set")
	}

	return &Service{
		bot:    bot,
		logger: logger,
	}, nil
}

// GetUpdatesChan возвращает канал обновлений
func (s *Service) GetUpdatesChan() tgbotapi.UpdatesChannel {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	return s.bot.GetUpdatesChan(u)
}

// SendMessage отправляет текстовое сообщение
func (s *Service) SendMessage(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := s.bot.Send(msg)

	return err
}

// SendHTMLMessage отправляет сообщение с HTML форматированием
func (s *Service) SendHTMLMessage(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	_, err := s.bot.Send(msg)

	return err
}

// GetFileDirectURL получает прямую ссылку на файл
func (s *Service) GetFileDirectURL(fileID string) (string, error) {
	return s.bot.GetFileDirectURL(fileID)
}

// GetBot возвращает экземпляр бота (для совместимости)
func (s *Service) GetBot() *tgbotapi.BotAPI {
	return s.bot
}
