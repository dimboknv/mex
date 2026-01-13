package copytrading

import (
	"context"
	"fmt"
	"log/slog"

	corecopytrade "tg_mexc/internal/mexc/copytrading"
	"tg_mexc/internal/mexc/copytrading/websocket"
)

// service реализует CopyTradingService
type service struct {
	manager   *corecopytrade.Manager
	storage   AccountStorage
	wsService *webSocketService
	mirrorSvc *mirrorService
	apiURL    string
	logger    *slog.Logger
}

// NewService создаёт главный сервис copy trading
func NewService(
	manager *corecopytrade.Manager,
	storage AccountStorage,
	apiURL string,
	logger *slog.Logger,
) CopyTradingService {
	wsSvc := &webSocketService{
		manager:     manager,
		storage:     storage,
		logger:      logger,
		connections: make(map[int]*wscopytrading.Service),
	}

	mirrorSvc := &mirrorService{
		manager: manager,
		storage: storage,
		apiURL:  apiURL,
		logger:  logger,
		tokens:  make(map[string]*mirrorToken),
		active:  make(map[int]bool),
	}

	return &service{
		manager:   manager,
		storage:   storage,
		wsService: wsSvc,
		mirrorSvc: mirrorSvc,
		apiURL:    apiURL,
		logger:    logger,
	}
}

func (s *service) SetMode(ctx context.Context, userID int, username string, mode Mode, opts ModeOptions) error {
	// Определяем текущий режим
	currentMode := s.getCurrentMode(userID)

	// Если режим тот же - ничего не делаем
	if currentMode == mode {
		if mode == ModeOff {
			return nil
		}
		return fmt.Errorf("mode %s already active", mode)
	}

	// Останавливаем текущий режим
	if err := s.stopCurrentMode(ctx, userID, currentMode); err != nil {
		return fmt.Errorf("failed to stop current mode: %w", err)
	}

	// Запускаем новый режим
	switch mode {
	case ModeOff:
		return nil
	case ModeWebSocket:
		return s.wsService.Start(ctx, userID, opts)
	case ModeMirror:
		_, err := s.mirrorSvc.Start(ctx, userID, username)
		return err
	default:
		return fmt.Errorf("unknown mode: %s", mode)
	}
}

func (s *service) GetStatus(ctx context.Context, userID int, username string) Status {
	status := Status{
		Mode:   s.getCurrentMode(userID),
		DryRun: s.manager.IsDryRun(),
	}

	// Получаем данные об аккаунтах
	master, err := s.storage.GetMasterAccount(userID)
	if err == nil {
		status.MasterName = master.Name
	}

	slaves, err := s.storage.GetSlaveAccounts(userID, false)
	if err == nil {
		status.ActiveSlaveCount = len(slaves)
	}

	// Mirror-specific данные
	if status.Mode == ModeMirror {
		status.MirrorToken = s.mirrorSvc.GetToken(userID, username)
		status.MirrorURL = s.apiURL
		status.MirrorScript = s.GetMirrorScript(userID, username)
	}

	return status
}

func (s *service) StopAll() {
	s.wsService.stopAll()
	s.mirrorSvc.stopAll()
	s.logger.Info("All copy trading sessions stopped")
}

func (s *service) GetMirrorScript(userID int, username string) string {
	token := s.mirrorSvc.GetToken(userID, username)
	return generateMirrorScript(s.apiURL, token)
}

func (s *service) ValidateMirrorToken(token string) (userID int, username string, ok bool) {
	return s.mirrorSvc.ValidateToken(token)
}

func (s *service) ProcessMirrorRequest(ctx context.Context, token string, path string, body []byte) error {
	return s.mirrorSvc.ProcessRequest(ctx, token, path, body)
}

// getCurrentMode возвращает текущий активный режим
func (s *service) getCurrentMode(userID int) Mode {
	if s.wsService.IsActive(userID) {
		return ModeWebSocket
	}
	if s.mirrorSvc.IsActive(userID) {
		return ModeMirror
	}
	return ModeOff
}

// stopCurrentMode останавливает текущий режим
func (s *service) stopCurrentMode(ctx context.Context, userID int, mode Mode) error {
	switch mode {
	case ModeWebSocket:
		return s.wsService.Stop(ctx, userID)
	case ModeMirror:
		return s.mirrorSvc.Stop(ctx, userID)
	default:
		return nil
	}
}

// generateMirrorScript генерирует JS скрипт для mirror режима
func generateMirrorScript(mirrorURL, token string) string {
	return `(function() {
    const MIRROR_BASE_URL = '` + mirrorURL + `';
    const MIRROR_TOKEN = '` + token + `';

    const iframe = document.createElement('iframe');
    iframe.style.display = 'none';
    document.body.appendChild(iframe);
    const c = iframe.contentWindow.console;

    const originalFetch = window.fetch;

    window.fetch = async function(...args) {
        const url = args[0] instanceof Request ? args[0].url : args[0];

        if (!url.includes('mexc.com/api/platform/futures/api/v1/')) {
            return originalFetch.apply(this, args);
        }

        const options = args[1] || {};
        const method = options.method || 'GET';

        if (method !== 'POST') {
            return originalFetch.apply(this, args);
        }

        const urlObj = new URL(url);
        const pathAndQuery = urlObj.pathname + urlObj.search;
        const mirrorFullURL = MIRROR_BASE_URL + pathAndQuery;

        const mirrorHeaders = { ...options.headers, 'X-Mirror-Token': MIRROR_TOKEN };
        const [response] = await Promise.all([
            originalFetch.apply(this, args),
            originalFetch(mirrorFullURL, {
                method: 'POST',
                headers: mirrorHeaders,
                body: options.body || null
            }).catch(err => c.warn('Mirror error:', err))
        ]);

        let requestBody = null;
        if (options.body) {
            try { requestBody = JSON.parse(options.body); } catch { requestBody = options.body; }
        }

        const clone = response.clone();
        let responseData = null;
        try { responseData = await clone.json(); } catch { responseData = await clone.text(); }

        c.group('🔵 ' + url);
        c.log('Method:', method);
        c.log('Request Body:', requestBody);
        c.log('Response:', responseData);
        c.log('Mirror URL:', mirrorFullURL);
        c.groupEnd();

        return response;
    };

    c.log('✅ MEXC Mirror interceptor ready (POST only)');
    c.log('📡 Mirror base:', MIRROR_BASE_URL);
})();`
}
