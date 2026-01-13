package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"tg_mexc/internal/api/middleware"
	"tg_mexc/internal/mexc"
	"tg_mexc/internal/models"

	"github.com/gorilla/mux"
)

type AddAccountRequest struct {
	Name        string             `json:"name"`
	BrowserData models.BrowserData `json:"browser_data"`
	Proxy       string             `json:"proxy,omitempty"`
}

type AccountResponse struct {
	ID       int     `json:"id"`
	Name     string  `json:"name"`
	Token    string  `json:"token"`
	DeviceID string  `json:"device_id"`
	Proxy    string  `json:"proxy,omitempty"`
	IsMaster bool    `json:"is_master"`
	Disabled bool    `json:"disabled"`
	MakerFee float64 `json:"maker_fee,omitempty"`
	TakerFee float64 `json:"taker_fee,omitempty"`
	Balance  float64 `json:"balance,omitempty"`
}

// HandleGetAccounts возвращает список всех аккаунтов пользователя
func (h *Handler) HandleGetAccounts(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	accounts, err := h.storage.GetAccounts(userID)
	if err != nil {
		h.logger.Error("Failed to get accounts", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to get accounts")

		return
	}

	// Преобразуем в response
	var response []AccountResponse
	for _, acc := range accounts {
		response = append(response, AccountResponse{
			ID:       acc.ID,
			Name:     acc.Name,
			Token:    acc.Token[:10] + "...", // Показываем только начало токена
			DeviceID: acc.DeviceID,
			Proxy:    acc.Proxy,
			IsMaster: acc.IsMaster,
			Disabled: acc.Disabled,
		})
	}

	h.respondSuccess(w, "", response)
}

// HandleGetAccountsWithDetails возвращает аккаунты с балансами и комиссиями
func (h *Handler) HandleGetAccountsWithDetails(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	accounts, err := h.storage.GetAccounts(userID)
	if err != nil {
		h.logger.Error("Failed to get accounts", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to get accounts")

		return
	}

	ctx := r.Context()
	var response []AccountResponse

	for _, acc := range accounts {
		client, err := mexc.NewClient(acc, h.logger)
		if err != nil {
			h.logger.Error("Failed to create MEXC client", "account", acc.Name, "error", err)
			continue
		}

		accResp := AccountResponse{
			ID:       acc.ID,
			Name:     acc.Name,
			Token:    acc.Token[:10] + "...",
			DeviceID: acc.DeviceID,
			Proxy:    acc.Proxy,
			IsMaster: acc.IsMaster,
			Disabled: acc.Disabled,
		}

		// Получаем баланс
		balances, err := client.GetBalance(ctx)
		if err == nil {
			for _, bal := range balances {
				if bal.Currency == "USDT" {
					accResp.Balance = bal.AvailableBalance
					break
				}
			}
		}

		// Получаем комиссии
		feeRate, err := client.GetTieredFeeRate(ctx, "")
		if err == nil {
			accResp.MakerFee = feeRate.OriginalMakerFee
			accResp.TakerFee = feeRate.OriginalTakerFee
		}

		response = append(response, accResp)
	}

	h.respondSuccess(w, "", response)
}

// HandleAddAccount добавляет новый аккаунт
func (h *Handler) HandleAddAccount(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	var req AddAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Валидация
	if req.Name == "" {
		h.respondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	if req.BrowserData.UcToken == "" || req.BrowserData.UID == "" || req.BrowserData.DeviceID == "" {
		h.respondError(w, http.StatusBadRequest, "Browser data is incomplete")
		return
	}

	// Проверяем, существует ли уже аккаунт с таким MEXC UID
	exists, err := h.storage.AccountExistsByMexcUID(userID, req.BrowserData.UID)
	if err != nil {
		h.logger.Error("Failed to check account existence", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to check account existence")
		return
	}
	if exists {
		h.respondError(w, http.StatusConflict, "Account with this MEXC UID already exists")
		return
	}

	// Добавляем аккаунт
	err = h.storage.AddAccount(userID, req.Name, req.BrowserData, req.Proxy)
	if err != nil {
		h.logger.Error("Failed to add account", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to add account")

		return
	}

	h.respondSuccess(w, "Account added successfully", nil)
}

// HandleDeleteAccount удаляет аккаунт
func (h *Handler) HandleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	vars := mux.Vars(r)
	accountID, err := strconv.Atoi(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid account ID")
		return
	}

	err = h.storage.DeleteAccount(userID, accountID)
	if err != nil {
		h.logger.Error("Failed to delete account", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to delete account")

		return
	}

	h.respondSuccess(w, "Account deleted successfully", nil)
}

// HandleSetMaster устанавливает аккаунт как главный
func (h *Handler) HandleSetMaster(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	vars := mux.Vars(r)
	accountID, err := strconv.Atoi(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid account ID")
		return
	}

	// Проверяем, не запущен ли copy trading
	isActive, err := h.storage.HasActiveCopyTradingSession(userID)
	if err != nil {
		h.logger.Error("Failed to check copy trading status", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to check copy trading status")

		return
	}

	if isActive {
		h.respondError(w, http.StatusConflict, "Cannot change master account while copy trading is active")
		return
	}

	err = h.storage.SetMasterAccount(userID, accountID)
	if err != nil {
		h.logger.Error("Failed to set master account", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to set master account")

		return
	}

	h.respondSuccess(w, "Master account set successfully", nil)
}

// HandleToggleDisabled включает/выключает аккаунт
func (h *Handler) HandleToggleDisabled(w http.ResponseWriter, r *http.Request) {
	userID, _ := middleware.GetUserID(r.Context())

	vars := mux.Vars(r)
	accountID, err := strconv.Atoi(vars["id"])
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid account ID")
		return
	}

	var req struct {
		Disabled bool `json:"disabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	err = h.storage.UpdateDisabledStatus(userID, accountID, req.Disabled)
	if err != nil {
		h.logger.Error("Failed to update disabled status", "error", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to update disabled status")

		return
	}

	h.respondSuccess(w, "Account status updated successfully", nil)
}

// HandleGetScript возвращает JS скрипт для извлечения данных из браузера
func (h *Handler) HandleGetScript(w http.ResponseWriter, r *http.Request) {
	script := `function downloadJSON(data, filename) {
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
    for (let i = 0; i < localStorage.length; i++) {
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

	h.respondSuccess(w, "", map[string]string{"script": script})
}
