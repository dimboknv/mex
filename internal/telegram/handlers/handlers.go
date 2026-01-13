package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tg_mexc/internal/mexc"
	"tg_mexc/internal/models"
	"tg_mexc/internal/storage"
	"tg_mexc/internal/telegram"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Handler обрабатывает команды бота
type Handler struct {
	storage     *storage.Storage
	telegram    *telegram.Service
	copyTrading interface {
		Start(chatID int64, ignoreFees bool) (string, error)
		Stop(chatID int64) (string, error)
		IsActive(chatID int64) bool
		GetStatus(chatID int64) string
		GetEventChannel(chatID int64) <-chan string
	}
	logger *slog.Logger
}

// New создает новый обработчик
func New(storage *storage.Storage, telegram *telegram.Service, copyTrading interface {
	Start(chatID int64, ignoreFees bool) (string, error)
	Stop(chatID int64) (string, error)
	IsActive(chatID int64) bool
	GetStatus(chatID int64) string
	GetEventChannel(chatID int64) <-chan string
}, logger *slog.Logger,
) *Handler {
	return &Handler{
		storage:     storage,
		telegram:    telegram,
		copyTrading: copyTrading,
		logger:      logger,
	}
}

// HandleUpdate обрабатывает обновление от Telegram
func (h *Handler) HandleUpdate(update tgbotapi.Update) {
	if update.Message == nil {
		return
	}

	// Создаем контекст с таймаутом 5 секунд
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	chatID := update.Message.Chat.ID

	// Обработка файлов для /add_browser
	if update.Message.Document != nil && update.Message.Caption != "" && strings.HasPrefix(update.Message.Caption, "/add_browser") {
		h.handleBrowserFileUpload(ctx, chatID, update.Message)
		return
	}

	if !update.Message.IsCommand() {
		return
	}

	cmd := update.Message.Command()
	args := strings.Fields(update.Message.CommandArguments())

	h.logger.Info("Command received",
		slog.Int64("chat_id", chatID),
		slog.String("command", cmd),
		slog.Any("args", args))

	var response string

	switch cmd {
	case "start":
		response = h.handleStart()
	case "script":
		h.handleScript(chatID)
		return
	case "add_browser":
		response = h.handleAddBrowser()
	case "delete", "remove":
		response = h.handleDelete(chatID, args)
	case "list":
		response = h.handleList(chatID)
	case "balance":
		response = h.handleBalance(ctx, chatID)
	case "fee_rates":
		response = h.handleFeeRates(ctx, chatID)
	case "open":
		response = h.handleOpen(ctx, chatID, args)
	case "close":
		response = h.handleClose(ctx, chatID, args)
	case "open_all":
		response = h.handleOpenAll(ctx, chatID, args)
	case "close_all":
		response = h.handleCloseAll(ctx, chatID, args)
	case "positions":
		response = h.handlePositions(ctx, chatID)
	case "open_orders":
		response = h.handleOpenOrders(ctx, chatID)
	case "open_stop_orders":
		response = h.handleOpenStopOrders(ctx, chatID)
	case "set_master":
		response = h.handleSetMaster(chatID, args)
	case "start_copy":
		response = h.handleStartCopy(chatID, args)
	case "stop_copy":
		response = h.handleStopCopy(chatID)
	case "copy_status":
		response = h.handleCopyStatus(chatID)
	case "help":
		response = h.handleHelp()
	default:
		response = "❌ Неизвестная команда. /help"
	}

	h.telegram.SendMessage(chatID, response)
}

func (h *Handler) handleStart() string {
	return `🌐 MEXC Copy Trading Bot (Browser Auth)

📋 Управление аккаунтами:
/script - Получить JS скрипт для браузера
/add_browser - Добавить аккаунт (через файл)
/delete <name> - Удалить аккаунт
/list - Список аккаунтов
/balance - Баланс
/fee_rates - Проверить комиссии

🔄 Copy Trading:
/set_master <name> - Установить главный аккаунт
/start_copy [ignore_fees] - Запустить копирование сделок
/stop_copy - Остановить копирование
/copy_status - Статус копирования

📊 Торговля (отдельный аккаунт):
/open <name> <symbol> <long|short> <vol> <leverage>
/close <name> <symbol>

🎯 Торговля (все аккаунты):
/open_all <symbol> <long|short> <vol> <leverage>
/close_all <symbol>

📈 Информация:
/positions - Позиции
/help - Помощь`
}

func (h *Handler) handleScript(chatID int64) {
	scriptText := `📜 JavaScript скрипт для извлечения данных

1. Открой https://www.mexc.com/futures
2. Войди в аккаунт
3. Открой DevTools (F12) → Console
4. Напиши: allow pasting
5. Скопируй и вставь этот скрипт:

<pre language="javascript">` + getExtractScript() + `</pre>

6. Файл mexc-data.json автоматически скачается
7. Прикрепи этот файл к сообщению в Telegram
8. В Caption напиши: /add_browser &lt;name&gt; [proxy]

Готово! Файл содержит все нужные данные.`

	h.telegram.SendHTMLMessage(chatID, scriptText)
}

func (h *Handler) handleAddBrowser() string {
	return `❌ Отправь данные файлом!

1. Выполни скрипт в браузере (/script)
2. Файл автоматически скачается на твой компьютер
3. Прикрепи этот файл к сообщению
4. В поле Caption напиши: /add_browser <name> [proxy]

Пример:
📎 mexc_data.json
Caption: /add_browser Main

С прокси:
📎 mexc_data.json
Caption: /add_browser Main http://proxy:8080`
}

func (h *Handler) handleDelete(chatID int64, args []string) string {
	if len(args) < 1 {
		return "❌ Формат: /delete <name>"
	}

	name := args[0]
	err := h.storage.DeleteAccount(chatID, name)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка: %v", err)
	}

	return fmt.Sprintf("✅ Аккаунт %s удален", name)
}

func (h *Handler) handleList(chatID int64) string {
	accounts, err := h.storage.GetAccounts(chatID)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка: %v", err)
	}

	if len(accounts) == 0 {
		return "📝 Нет аккаунтов. /add_browser"
	}

	var lines []string
	lines = append(lines, "📋 АККАУНТЫ:\n")

	for i, acc := range accounts {
		position := fmt.Sprintf("#%d", i+1)

		proxyInfo := ""
		if acc.Proxy != "" {
			proxyInfo = fmt.Sprintf("\nProxy: %s", acc.Proxy)
		}

		disabledIcon := ""
		if acc.Disabled {
			disabledIcon = " 🛑"
		}

		lines = append(lines, fmt.Sprintf("%s %s%s\nToken: %s...\nDevice: %s...%s\n",
			position, acc.Name, disabledIcon, acc.Token[:10], acc.DeviceID[:8], proxyInfo))
	}

	return strings.Join(lines, "\n")
}

func (h *Handler) handleBalance(ctx context.Context, chatID int64) string {
	accounts, err := h.storage.GetAccounts(chatID)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка: %v", err)
	}

	var lines []string
	lines = append(lines, "💰 БАЛАНС:\n")

	totalUSDT := 0.0

	for _, acc := range accounts {
		client, err := mexc.NewClient(acc, h.logger)
		if err != nil {
			lines = append(lines, fmt.Sprintf("❌ %s: ошибка\n", acc.Name))
			continue
		}

		balances, err := client.GetBalance(ctx)
		if err != nil {
			lines = append(lines, fmt.Sprintf("❌ %s: %v\n", acc.Name, err))
			continue
		}

		for _, bal := range balances {
			if bal.Currency == "USDT" {
				lines = append(lines, fmt.Sprintf("%s: %.2f USDT\n", acc.Name, bal.AvailableBalance))
				totalUSDT += bal.AvailableBalance
			}
		}
	}

	lines = append(lines, fmt.Sprintf("\nВсего: %.2f USDT", totalUSDT))

	return strings.Join(lines, "")
}

func (h *Handler) handleOpen(ctx context.Context, chatID int64, args []string) string {
	if len(args) < 5 {
		return "❌ Формат: /open <name> <symbol> <long|short> <vol> <leverage>"
	}

	accountName := args[0]
	symbol := strings.ToUpper(args[1])
	sideStr := strings.ToLower(args[2])
	vol, _ := strconv.Atoi(args[3])
	leverage, _ := strconv.Atoi(args[4])

	side := 1 // 1=open long
	if sideStr == "short" {
		side = 3 // 3=open short
	}

	// Получаем все аккаунты пользователя
	accounts, err := h.storage.GetAccounts(chatID)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка: %v", err)
	}

	// Ищем нужный аккаунт
	var targetAccount *models.Account
	for _, acc := range accounts {
		if acc.Name == accountName {
			targetAccount = &acc
			break
		}
	}

	if targetAccount == nil {
		return fmt.Sprintf("❌ Аккаунт '%s' не найден. Используй /list", accountName)
	}

	// Проверяем disabled статус
	// if targetAccount.Disabled {
	// 	return fmt.Sprintf("🛑 Аккаунт '%s' отключен из-за наличия комиссии. Торговля невозможна.", accountName)
	// }

	// Создаём клиент и открываем позицию
	client, err := mexc.NewClient(*targetAccount, h.logger)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка создания клиента: %v", err)
	}

	_, err = client.PlaceOrder(ctx, symbol, side, vol, leverage)
	if err != nil {
		h.logger.Error("Order failed",
			slog.String("account", targetAccount.Name),
			slog.Any("error", err))

		return fmt.Sprintf("❌ Ошибка открытия позиции на %s: %v", accountName, err)
	}

	h.logger.Info("✅ Order placed",
		slog.String("account", targetAccount.Name))

	// Проверяем и обновляем disabled статус после открытия позиции
	h.checkAndUpdateDisabledStatus(ctx, chatID, accountName)

	sideStr = "LONG"
	if side == 3 {
		sideStr = "SHORT"
	}

	return fmt.Sprintf(`✅ ПОЗИЦИЯ ОТКРЫТА

Аккаунт: %s
Symbol: %s %s x%d
Volume: %d`,
		accountName, symbol, sideStr, leverage, vol)
}

func (h *Handler) handleClose(ctx context.Context, chatID int64, args []string) string {
	if len(args) < 2 {
		return "❌ Формат: /close <name> <symbol>"
	}

	accountName := args[0]
	symbol := strings.ToUpper(args[1])

	// Получаем все аккаунты пользователя
	accounts, err := h.storage.GetAccounts(chatID)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка: %v", err)
	}

	// Ищем нужный аккаунт
	var targetAccount *models.Account
	for _, acc := range accounts {
		if acc.Name == accountName {
			targetAccount = &acc
			break
		}
	}

	if targetAccount == nil {
		return fmt.Sprintf("❌ Аккаунт '%s' не найден. Используй /list", accountName)
	}

	// Создаём клиент и закрываем позицию
	client, err := mexc.NewClient(*targetAccount, h.logger)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка создания клиента: %v", err)
	}

	err = client.ClosePosition(ctx, symbol)
	if err != nil {
		h.logger.Error("Close failed",
			slog.String("account", targetAccount.Name),
			slog.Any("error", err))

		return fmt.Sprintf("❌ Ошибка закрытия позиции на %s: %v", accountName, err)
	}

	return fmt.Sprintf(`✅ ПОЗИЦИЯ ЗАКРЫТА

Аккаунт: %s
Symbol: %s`,
		accountName, symbol)
}

func (h *Handler) handleOpenAll(ctx context.Context, chatID int64, args []string) string {
	if len(args) < 4 {
		return "❌ Формат: /open_all <symbol> <long|short> <vol> <leverage>"
	}

	symbol := strings.ToUpper(args[0])
	sideStr := strings.ToLower(args[1])
	vol, _ := strconv.Atoi(args[2])
	leverage, _ := strconv.Atoi(args[3])

	side := 1 // 1=open long
	if sideStr == "short" {
		side = 3 // 3=open short
	}

	accounts, err := h.storage.GetAccounts(chatID)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка: %v", err)
	}

	if len(accounts) == 0 {
		return "❌ Нет аккаунтов. /add_browser"
	}

	h.telegram.SendMessage(chatID, fmt.Sprintf("⏳ Открываю на %d аккаунтах...", len(accounts)))

	successCount := 0
	failedCount := 0
	skippedCount := 0

	for _, acc := range accounts {
		// Пропускаем disabled аккаунты
		// if acc.Disabled {
		// 	h.logger.Info("Skipping disabled account",
		// 		slog.String("account", acc.Name))
		//
		// 	skippedCount++
		//
		// 	continue
		// }

		client, err := mexc.NewClient(acc, h.logger)
		if err != nil {
			h.logger.Error("Account error",
				slog.String("account", acc.Name),
				slog.Any("error", err))

			failedCount++

			continue
		}

		_, err = client.PlaceOrder(ctx, symbol, side, vol, leverage)
		if err != nil {
			h.logger.Error("Order failed",
				slog.String("account", acc.Name),
				slog.Any("error", err))

			failedCount++
		} else {
			h.logger.Info("✅ Order placed",
				slog.String("account", acc.Name))

			successCount++

			// Проверяем и обновляем disabled статус после открытия позиции
			h.checkAndUpdateDisabledStatus(ctx, chatID, acc.Name)
		}

		time.Sleep(100 * time.Millisecond)
	}

	sideStr = "LONG"
	if side == 3 {
		sideStr = "SHORT"
	}

	skippedInfo := ""
	if skippedCount > 0 {
		skippedInfo = fmt.Sprintf("\n🛑 Пропущено (disabled): %d", skippedCount)
	}

	return fmt.Sprintf(`✅ ПОЗИЦИЯ ОТКРЫТА

Symbol: %s %s x%d
Volume: %d

✅ Успешно: %d/%d
❌ Ошибки: %d%s`,
		symbol, sideStr, leverage, vol,
		successCount, len(accounts),
		failedCount, skippedInfo)
}

func (h *Handler) handleCloseAll(ctx context.Context, chatID int64, args []string) string {
	if len(args) < 1 {
		return "❌ Формат: /close_all <symbol>"
	}

	symbol := strings.ToUpper(args[0])

	accounts, err := h.storage.GetAccounts(chatID)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка: %v", err)
	}

	h.telegram.SendMessage(chatID, fmt.Sprintf("⏳ Закрываю %s на %d аккаунтах...", symbol, len(accounts)))

	successCount := 0
	failedCount := 0

	for _, acc := range accounts {
		client, err := mexc.NewClient(acc, h.logger)
		if err != nil {
			failedCount++
			continue
		}

		err = client.ClosePosition(ctx, symbol)
		if err != nil {
			h.logger.Error("Close failed",
				slog.String("account", acc.Name),
				slog.Any("error", err))

			failedCount++
		} else {
			successCount++
		}

		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Sprintf(`✅ ПОЗИЦИЯ ЗАКРЫТА

Symbol: %s
✅ Успешно: %d/%d
❌ Ошибки: %d`,
		symbol,
		successCount, len(accounts),
		failedCount)
}

func (h *Handler) handlePositions(ctx context.Context, chatID int64) string {
	accounts, err := h.storage.GetAccounts(chatID)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка: %v", err)
	}

	var lines []string
	lines = append(lines, "📊 ОТКРЫТЫЕ ПОЗИЦИИ:\n")

	hasPositions := false

	for _, acc := range accounts {
		client, err := mexc.NewClient(acc, h.logger)
		if err != nil {
			continue
		}

		positions, err := client.GetPositions(ctx, "")
		if err != nil {
			continue
		}

		if len(positions) > 0 {
			hasPositions = true

			lines = append(lines, fmt.Sprintf("\n%s:", acc.Name))

			for _, pos := range positions {
				posType := "LONG"
				if pos.PositionType == 2 {
					posType = "SHORT"
				}

				lines = append(lines, fmt.Sprintf("  %s %s x%d - %.0f @ %.2f",
					pos.Symbol, posType, pos.Leverage, pos.HoldVol, pos.HoldAvgPrice))
			}
		}
	}

	if !hasPositions {
		return "📊 Нет открытых позиций"
	}

	return strings.Join(lines, "\n")
}

func (h *Handler) handleHelp() string {
	return `📖 ПОМОЩЬ

📋 Добавление аккаунта (только через файл!):
1. Получи скрипт: /script
2. Зайди на MEXC в браузере (https://www.mexc.com/futures)
3. Открой DevTools (F12) → Console
4. Напиши: allow pasting
5. Вставь скрипт и нажми Enter
6. Файл автоматически скачается на компьютер
7. Прикрепи файл к сообщению
8. В Caption напиши: /add_browser <name> [proxy]

Примеры:
📎 mexc_data.json
Caption: /add_browser Main

📎 mexc_data.json
Caption: /add_browser Acc1 http://proxy:8080

Управление:
/list - список аккаунтов
/delete <name> - удалить аккаунт
/balance - баланс
/fee_rates - проверить комиссии

🔄 Copy Trading:
/set_master Main - установить Main как главный аккаунт
/start_copy - запустить копирование (только аккаунты без комиссии)
/start_copy ignore_fees - запустить с игнорированием комиссий (все аккаунты)
/stop_copy - остановить копирование
/copy_status - проверить статус копирования

📊 Торговля (отдельный аккаунт):
/open Main BTC_USDT long 100 20 - открыть long на Main
/open Acc1 ETH_USDT short 50 10 - открыть short на Acc1
/close Main BTC_USDT - закрыть BTC на Main

🎯 Торговля (все аккаунты):
/open_all BTC_USDT long 100 20 - открыть long на всех
/open_all ETH_USDT short 50 10 - открыть short на всех
/close_all BTC_USDT - закрыть BTC на всех

📈 Информация:
/positions - показать позиции`
}

func (h *Handler) handleBrowserFileUpload(ctx context.Context, chatID int64, msg *tgbotapi.Message) {
	parts := strings.Fields(msg.Caption)
	if len(parts) < 2 {
		h.telegram.SendMessage(chatID, "❌ Формат: отправь файл с caption /add_browser <name> [proxy]")
		return
	}

	name := parts[1]
	proxyStr := ""
	if len(parts) > 2 {
		proxyStr = parts[2]
	}

	fileURL, err := h.telegram.GetFileDirectURL(msg.Document.FileID)
	if err != nil {
		h.telegram.SendMessage(chatID, fmt.Sprintf("❌ Ошибка скачивания файла: %v", err))
		return
	}

	resp, err := http.Get(fileURL)
	if err != nil {
		h.telegram.SendMessage(chatID, fmt.Sprintf("❌ Ошибка загрузки: %v", err))
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var data models.BrowserData
	err = json.Unmarshal(body, &data)
	if err != nil {
		h.telegram.SendMessage(chatID, fmt.Sprintf("❌ Invalid JSON: %v", err))
		return
	}

	err = h.storage.AddAccount(chatID, name, data, proxyStr)
	if err != nil {
		h.telegram.SendMessage(chatID, fmt.Sprintf("❌ Ошибка: %v", err))
		return
	}

	proxyInfo := ""
	if proxyStr != "" {
		proxyInfo = fmt.Sprintf("\nProxy: %s", proxyStr)
	}

	// Проверяем fee rate и обновляем disabled статус
	hasCommission := h.checkAndUpdateDisabledStatus(ctx, chatID, name)

	disabledWarning := ""
	if hasCommission {
		disabledWarning = "\n\n🛑 ВНИМАНИЕ: На аккаунте есть комиссия! Аккаунт отключен для торговли."
	}

	h.telegram.SendMessage(chatID, fmt.Sprintf("✅ Аккаунт %s добавлен из файла!\nToken: %s...\nUser ID: %s\nDevice: %s...%s%s",
		name, data.UcToken[:10], data.UID, data.DeviceID[:8], proxyInfo, disabledWarning))
}

func getExtractScript() string {
	return `function downloadJSON(data, filename) {
    const blob = new Blob([JSON.stringify(data, null, 2)], {type: 'application/json'});
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = filename;
    link.click();
    URL.revokeObjectURL(url);
}

function extractCompleteData() {
    const cookies = {};
    document.cookie.split(';').forEach(cookie => {
        const [key, value] = cookie.trim().split('=');
        if (key && value) {
            try {
                cookies[key] = decodeURIComponent(value);
            } catch(e) {
                cookies[key] = value;
            }
        }
    });

    const storage = {};
    for (let i = 0; i &lt; localStorage.length; i++) {
        const key = localStorage.key(i);
        storage[key] = localStorage.getItem(key);
    }

    const criticalCookies = {
        uc_token: cookies.uc_token,
        u_id: cookies.u_id,
        cslfp: cookies.cslfp,
        sensorsdata2015jssdkcross: cookies.sensorsdata2015jssdkcross,
        _abck: cookies._abck,
        bm_sz: cookies.bm_sz,
        bm_sv: cookies.bm_sv
    };

    const data = {
        uc_token: cookies.uc_token || '',
        u_id: cookies.u_id || '',
        deviceId: storage['mexc_fingerprint_visitorId'] ||
                  cookies['mexc_fingerprint_visitorId'] || '',
        allCookies: criticalCookies,
        userAgent: navigator.userAgent,
        timezone: Intl.DateTimeFormat().resolvedOptions().timeZone
    };

    console.log('✅ Данные собраны!');
    console.log('Размер:', JSON.stringify(data).length, 'символов');

    downloadJSON(data, 'mexc-data.json');

    return data;
}

extractCompleteData();`
}

func (h *Handler) handleSetMaster(chatID int64, args []string) string {
	if len(args) < 1 {
		return "❌ Формат: /set_master <name>"
	}

	name := args[0]
	err := h.storage.SetMasterAccount(chatID, name)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка: %v", err)
	}

	return fmt.Sprintf("✅ Аккаунт %s установлен как главный для copy trading", name)
}

func (h *Handler) handleStartCopy(chatID int64, args []string) string {
	// По умолчанию не игнорируем комиссию
	ignoreFees := false

	// Проверяем аргументы
	if len(args) > 0 {
		if args[0] == "ignore_fees" || args[0] == "ignore" {
			ignoreFees = true
		}
	}

	msg, err := h.copyTrading.Start(chatID, ignoreFees)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка: %v", err)
	}

	go func() {
		for msg := range h.copyTrading.GetEventChannel(chatID) {
			h.telegram.SendMessage(chatID, msg)
		}
	}()

	return msg
}

func (h *Handler) handleStopCopy(chatID int64) string {
	msg, err := h.copyTrading.Stop(chatID)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка: %v", err)
	}

	return msg
}

func (h *Handler) handleCopyStatus(chatID int64) string {
	return h.copyTrading.GetStatus(chatID)
}

func (h *Handler) handleOpenOrders(ctx context.Context, chatID int64) string {
	accounts, err := h.storage.GetAccounts(chatID)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка: %v", err)
	}

	var lines []string
	lines = append(lines, "📋 ОТКРЫТЫЕ ОРДЕРА:\n")

	hasOrders := false

	for _, acc := range accounts {
		client, err := mexc.NewClient(acc, h.logger)
		if err != nil {
			continue
		}

		orders, err := client.GetOpenOrders(ctx, 1, 100)
		if err != nil {
			continue
		}

		if len(orders) > 0 {
			hasOrders = true

			lines = append(lines, fmt.Sprintf("\n%s:", acc.Name))

			for _, order := range orders {
				sideText := ""
				switch order.Side {
				case 1:
					sideText = "OPEN LONG"
				case 2:
					sideText = "CLOSE SHORT"
				case 3:
					sideText = "OPEN SHORT"
				case 4:
					sideText = "CLOSE LONG"
				}

				stateText := ""
				switch order.State {
				case 1:
					stateText = "Pending"
				case 2:
					stateText = "Unfilled"
				case 3:
					stateText = "Filled"
				case 4:
					stateText = "Canceled"
				case 5:
					stateText = "Invalid"
				}

				lines = append(lines, fmt.Sprintf("  %s %s x%d\n  Vol: %.0f @ %.2f\n  State: %s\n  ID: %s",
					order.Symbol, sideText, order.Leverage, order.Vol, order.Price, stateText, order.OrderID))
			}
		}
	}

	if !hasOrders {
		return "📋 Нет открытых ордеров"
	}

	return strings.Join(lines, "\n")
}

func (h *Handler) handleOpenStopOrders(ctx context.Context, chatID int64) string {
	accounts, err := h.storage.GetAccounts(chatID)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка: %v", err)
	}

	var lines []string
	lines = append(lines, "🎯 СТОП-ОРДЕРА:\n")

	hasOrders := false

	for _, acc := range accounts {
		client, err := mexc.NewClient(acc, h.logger)
		if err != nil {
			continue
		}

		stopOrders, err := client.GetOpenStopOrders(ctx, "")
		if err != nil {
			continue
		}

		if len(stopOrders) > 0 {
			hasOrders = true

			lines = append(lines, fmt.Sprintf("\n%s:", acc.Name))

			for _, order := range stopOrders {
				stateText := ""
				switch order.State {
				case 1:
					stateText = "Active"
				case 2:
					stateText = "Triggered"
				case 3:
					stateText = "Canceled"
				}

				lines = append(lines, fmt.Sprintf("  %s\n  SL: %.2f | TP: %d\n  State: %s\n  ID: %s",
					order.Symbol, order.StopLossPrice, order.TakeProfitReverse, stateText, order.OrderId))
			}
		}
	}

	if !hasOrders {
		return "🎯 Нет стоп-ордеров"
	}

	return strings.Join(lines, "\n")
}

// checkAndUpdateDisabledStatus проверяет fee rate и обновляет disabled статус
func (h *Handler) checkAndUpdateDisabledStatus(ctx context.Context, chatID int64, accountName string) bool {
	accounts, err := h.storage.GetAccounts(chatID)
	if err != nil {
		return false
	}

	var targetAccount *models.Account
	for _, acc := range accounts {
		if acc.Name == accountName {
			targetAccount = &acc
			break
		}
	}

	if targetAccount == nil {
		return false
	}

	client, err := mexc.NewClient(*targetAccount, h.logger)
	if err != nil {
		return false
	}

	feeRate, err := client.GetTieredFeeRate(ctx, "")
	if err != nil {
		return false
	}

	// disabled = true если есть комиссия (не равна 0)
	hasCommission := feeRate.OriginalMakerFee != 0 || feeRate.OriginalTakerFee != 0

	// Обновляем статус в БД
	h.storage.UpdateDisabledStatus(chatID, accountName, hasCommission)

	return hasCommission
}

func (h *Handler) handleFeeRates(ctx context.Context, chatID int64) string {
	accounts, err := h.storage.GetAccounts(chatID)
	if err != nil {
		return fmt.Sprintf("❌ Ошибка: %v", err)
	}

	if len(accounts) == 0 {
		return "📝 Нет аккаунтов. /add_browser"
	}

	var lines []string
	lines = append(lines, "💸 КОМИССИИ:\n")

	for _, acc := range accounts {
		client, err := mexc.NewClient(acc, h.logger)
		if err != nil {
			lines = append(lines, fmt.Sprintf("❌ %s: ошибка\n", acc.Name))
			continue
		}

		feeRate, err := client.GetTieredFeeRate(ctx, "")
		if err != nil {
			lines = append(lines, fmt.Sprintf("❌ %s: %v\n", acc.Name, err))
			continue
		}

		warningIcon := ""
		if feeRate.OriginalMakerFee != 0 || feeRate.OriginalTakerFee != 0 {
			warningIcon = " 🛑"
		}

		lines = append(lines, fmt.Sprintf("%s:%s\n  Maker: %.4f%%\n  Taker: %.4f%%\n",
			acc.Name, warningIcon, feeRate.OriginalMakerFee*100, feeRate.OriginalTakerFee*100))
	}

	return strings.Join(lines, "")
}
