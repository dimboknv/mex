// API Configuration (loaded from /config.js)
const API_URL = window.APP_CONFIG?.API_URL || '';

// State
let token = localStorage.getItem('token');
let refreshToken = localStorage.getItem('refreshToken');
let username = localStorage.getItem('username');
let currentPage = 'accounts';
let balancesInterval = null;
let feedInterval = null;
let isRefreshing = false;
let refreshQueue = [];
let selectedAccountIds = new Set();

// Fetch wrapper с автоматическим обновлением токена
async function apiFetch(url, options = {}) {
    // Добавляем Authorization header если есть токен
    if (token) {
        options.headers = {
            ...options.headers,
            'Authorization': `Bearer ${token}`
        };
    }

    let response = await fetch(url, options);

    // Если 401 и есть refresh token - пробуем обновить
    if (response.status === 401 && refreshToken) {
        // Предотвращаем множественные refresh запросы
        if (isRefreshing) {
            return new Promise((resolve, reject) => {
                refreshQueue.push({ resolve, reject, url, options });
            });
        }

        isRefreshing = true;

        try {
            const refreshResponse = await fetch(`${API_URL}/api/auth/refresh`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ refresh_token: refreshToken })
            });

            if (refreshResponse.ok) {
                const data = await refreshResponse.json();
                token = data.data.token;
                refreshToken = data.data.refresh_token;
                localStorage.setItem('token', token);
                localStorage.setItem('refreshToken', refreshToken);

                // Повторяем оригинальный запрос с новым токеном
                options.headers['Authorization'] = `Bearer ${token}`;
                response = await fetch(url, options);

                // Обрабатываем очередь ожидающих запросов
                refreshQueue.forEach(({ resolve, url: qUrl, options: qOpts }) => {
                    qOpts.headers['Authorization'] = `Bearer ${token}`;
                    resolve(fetch(qUrl, qOpts));
                });
                refreshQueue = [];
            } else {
                // Refresh token тоже невалиден - выходим
                handleLogout();
                refreshQueue.forEach(({ reject }) => reject(new Error('Session expired')));
                refreshQueue = [];
            }
        } catch (error) {
            handleLogout();
            refreshQueue.forEach(({ reject }) => reject(error));
            refreshQueue = [];
        } finally {
            isRefreshing = false;
        }
    } else if (response.status === 401) {
        // Нет refresh token - выходим
        handleLogout();
    }

    return response;
}

// DOM Elements
const loginContainer = document.getElementById('login-container');
const appContainer = document.getElementById('app-container');
const loginForm = document.getElementById('login-form');
const loginError = document.getElementById('login-error');
const usernameDisplay = document.getElementById('username-display');
const logoutBtn = document.getElementById('logout-btn');

// Initialize
document.addEventListener('DOMContentLoaded', () => {
    if (token) {
        showApp();
    } else {
        showLogin();
    }

    setupEventListeners();
});

function setupEventListeners() {
    console.log('setupEventListeners started');
    // Login
    loginForm.addEventListener('submit', handleLogin);
    document.getElementById('show-register')?.addEventListener('click', handleRegister);
    logoutBtn.addEventListener('click', handleLogout);

    // Navigation
    document.querySelectorAll('.nav-item').forEach(item => {
        item.addEventListener('click', (e) => {
            e.preventDefault();
            const page = e.target.dataset.page;
            switchPage(page);
        });
    });

    // Accounts
    document.getElementById('add-account-btn').addEventListener('click', showAddAccountModal);
    document.getElementById('load-details-btn').addEventListener('click', loadAccountsWithDetails);
    document.getElementById('close-modal-btn').addEventListener('click', hideAddAccountModal);
    document.getElementById('add-account-form').addEventListener('submit', handleAddAccount);
    document.getElementById('copy-script-btn').addEventListener('click', copyScript);

    // Copy Trading Mode Selection
    document.querySelectorAll('input[name="copy-mode"]').forEach(radio => {
        radio.addEventListener('change', handleModeChange);
    });

    // Mirror script copy button
    document.getElementById('copy-mirror-script-btn').addEventListener('click', copyMirrorScript);

    // Trades & Logs
    document.getElementById('refresh-trades-btn').addEventListener('click', loadTrades);
    document.getElementById('refresh-logs-btn').addEventListener('click', loadLogs);

    // Account History Modal
    document.getElementById('close-history-modal-btn').addEventListener('click', hideAccountHistoryModal);
}

// Auth Functions
async function handleLogin(e) {
    e.preventDefault();
    const usernameInput = document.getElementById('username').value;
    const password = document.getElementById('password').value;

    try {
        const response = await fetch(`${API_URL}/api/auth/login`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username: usernameInput, password })
        });

        const data = await response.json();

        if (response.ok) {
            token = data.data.token;
            refreshToken = data.data.refresh_token;
            username = data.data.username;
            localStorage.setItem('token', token);
            localStorage.setItem('refreshToken', refreshToken);
            localStorage.setItem('username', username);
            showApp();
        } else {
            showError(data.error || 'Login failed');
        }
    } catch (error) {
        showError('Connection error: ' + error.message);
    }
}

async function handleRegister(e) {
    e.preventDefault();
    const usernameInput = prompt('Введите username:');
    const password = prompt('Введите пароль (минимум 6 символов):');

    if (!usernameInput || !password) return;

    try {
        const response = await fetch(`${API_URL}/api/auth/register`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username: usernameInput, password })
        });

        const data = await response.json();

        if (response.ok) {
            token = data.data.token;
            refreshToken = data.data.refresh_token;
            username = data.data.username;
            localStorage.setItem('token', token);
            localStorage.setItem('refreshToken', refreshToken);
            localStorage.setItem('username', username);
            showApp();
        } else {
            alert('Ошибка регистрации: ' + (data.error || 'Unknown error'));
        }
    } catch (error) {
        alert('Connection error: ' + error.message);
    }
}

function handleLogout() {
    stopBalancesAutoRefresh();
    stopFeedAutoRefresh();
    // Инвалидируем refresh token на сервере
    if (refreshToken) {
        fetch(`${API_URL}/api/auth/logout`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ refresh_token: refreshToken })
        }).catch(() => {}); // Игнорируем ошибки
    }
    localStorage.removeItem('token');
    localStorage.removeItem('refreshToken');
    localStorage.removeItem('username');
    token = null;
    refreshToken = null;
    username = null;
    selectedAccountIds.clear();
    showLogin();
}

function showLogin() {
    loginContainer.classList.remove('hidden');
    appContainer.classList.add('hidden');
}

function showApp() {
    loginContainer.classList.add('hidden');
    appContainer.classList.remove('hidden');
    usernameDisplay.textContent = username;
    loadAccounts();
    startBalancesAutoRefresh();
    startFeedAutoRefresh();
}

function showError(message) {
    loginError.textContent = message;
    loginError.classList.remove('hidden');
    setTimeout(() => {
        loginError.classList.add('hidden');
    }, 5000);
}

// Navigation
function switchPage(page) {
    currentPage = page;

    // Update nav
    document.querySelectorAll('.nav-item').forEach(item => {
        item.classList.toggle('active', item.dataset.page === page);
    });

    // Update pages
    document.querySelectorAll('.page').forEach(p => {
        p.classList.toggle('active', p.id === `page-${page}`);
    });

    // Stop auto-refresh when leaving accounts page
    if (page !== 'accounts') {
        stopBalancesAutoRefresh();
        stopFeedAutoRefresh();
    }

    // Load data for page
    if (page === 'accounts') {
        loadAccounts();
        startBalancesAutoRefresh();
        startFeedAutoRefresh();
    }
    if (page === 'websocket') loadUnifiedStatus();
    if (page === 'trades') loadTrades();
    if (page === 'logs') loadLogs();
}

// Accounts
async function loadAccounts(withDetails = false) {
    try {
        const endpoint = withDetails ? '/api/accounts/details' : '/api/accounts';
        const response = await apiFetch(`${API_URL}${endpoint}`);

        const data = await response.json();

        if (response.ok) {
            renderAccounts(data.data || [], withDetails);
        }
    } catch (error) {
        console.error('Failed to load accounts:', error);
    }
}

async function loadAccountsWithDetails() {
    await loadAccounts(true);
}

function startBalancesAutoRefresh() {
    // Clear existing interval if any
    stopBalancesAutoRefresh();
    // Start auto-refresh every 5 seconds
    balancesInterval = setInterval(() => {
        loadAccounts(true);
    }, 5000);
    console.log('💰 Balances auto-refresh started (5s)');
}

function stopBalancesAutoRefresh() {
    if (balancesInterval) {
        clearInterval(balancesInterval);
        balancesInterval = null;
        console.log('💰 Balances auto-refresh stopped');
    }
}

function renderAccounts(accounts, withDetails = false) {
    const container = document.getElementById('accounts-list');

    if (!accounts || accounts.length === 0) {
        container.innerHTML = '<p>Нет аккаунтов. Добавьте первый аккаунт.</p>';
        return;
    }

    container.innerHTML = accounts.map(acc => `
        <div class="account-card ${acc.is_master ? 'master' : ''} ${acc.disabled ? 'disabled' : ''} ${selectedAccountIds.has(acc.id) ? 'selected' : ''}"
             onclick="toggleAccountSelection(${acc.id})" data-account-id="${acc.id}">
            <div class="account-header">
                <div class="account-name">${acc.name}</div>
                <div>
                    ${acc.is_master ? '<span class="account-badge badge-master">Master</span>' : ''}
                    ${acc.disabled ? '<span class="account-badge badge-disabled">Disabled</span>' : '<span class="account-badge badge-enabled">Active</span>'}
                    ${withDetails && (acc.maker_fee > 0 || acc.taker_fee > 0) ? '<span class="account-badge badge-fee">Fee</span>' : ''}
                </div>
            </div>
            <div class="account-info">
                ${withDetails ? `
                    <div><strong>Balance:</strong> ${acc.balance?.toFixed(2) || '—'} USDT</div>
                    <div><strong>Maker Fee:</strong> ${((acc.maker_fee || 0) * 100).toFixed(4)}%</div>
                    <div><strong>Taker Fee:</strong> ${((acc.taker_fee || 0) * 100).toFixed(4)}%</div>
                ` : ''}
                <div><strong>Token:</strong> ${acc.token}...</div>
            </div>
            <div class="account-actions">
                <button class="btn-info btn-small" onclick="event.stopPropagation(); showAccountHistory(${acc.id}, '${acc.name}', ${acc.is_master})">History</button>
                ${!acc.is_master ? `<button class="btn-primary btn-small" onclick="event.stopPropagation(); setMaster(${acc.id})">Set Master</button>` : ''}
                <button class="btn-${acc.disabled ? 'success' : 'secondary'} btn-small" onclick="event.stopPropagation(); toggleDisabled(${acc.id}, ${!acc.disabled})">${acc.disabled ? 'Enable' : 'Disable'}</button>
                <button class="btn-danger btn-small" onclick="event.stopPropagation(); deleteAccount(${acc.id})">Delete</button>
            </div>
        </div>
    `).join('');
}

// Toggle account selection
function toggleAccountSelection(accountId) {
    if (selectedAccountIds.has(accountId)) {
        selectedAccountIds.delete(accountId);
    } else {
        selectedAccountIds.add(accountId);
    }
    // Обновляем визуальное состояние
    const card = document.querySelector(`[data-account-id="${accountId}"]`);
    if (card) {
        card.classList.toggle('selected', selectedAccountIds.has(accountId));
    }
    // Обновляем ленту с учётом выбранных аккаунтов
    loadTradesFeed();
}

async function setMaster(accountId) {
    try {
        const response = await apiFetch(`${API_URL}/api/accounts/${accountId}/master`, {
            method: 'PUT'
        });

        if (response.ok) {
            loadAccounts();
        }
    } catch (error) {
        console.error('Failed to set master:', error);
    }
}

async function toggleDisabled(accountId, disabled) {
    try {
        const response = await apiFetch(`${API_URL}/api/accounts/${accountId}/disabled`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ disabled })
        });

        if (response.ok) {
            loadAccounts();
        }
    } catch (error) {
        console.error('Failed to toggle disabled:', error);
    }
}

async function deleteAccount(accountId) {
    if (!confirm('Вы уверены что хотите удалить этот аккаунт?')) return;

    try {
        const response = await apiFetch(`${API_URL}/api/accounts/${accountId}`, {
            method: 'DELETE'
        });

        if (response.ok) {
            loadAccounts();
        }
    } catch (error) {
        console.error('Failed to delete account:', error);
    }
}

// Add Account Modal
async function showAddAccountModal() {
    // Load script
    const response = await apiFetch(`${API_URL}/api/accounts/script`);
    const data = await response.json();
    document.getElementById('browser-script').textContent = data.data.script;

    document.getElementById('add-account-modal').classList.remove('hidden');
}

function hideAddAccountModal() {
    document.getElementById('add-account-modal').classList.add('hidden');
    document.getElementById('add-account-form').reset();
}

async function handleAddAccount(e) {
    e.preventDefault();

    const name = document.getElementById('account-name').value;
    const proxy = document.getElementById('account-proxy').value;
    const jsonStr = document.getElementById('account-json').value;

    try {
        const browserData = JSON.parse(jsonStr);

        const response = await apiFetch(`${API_URL}/api/accounts`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                name,
                proxy,
                browser_data: browserData
            })
        });

        if (response.ok) {
            hideAddAccountModal();
            loadAccounts();
        } else {
            const data = await response.json();
            alert('Error: ' + (data.error || 'Failed to add account'));
        }
    } catch (error) {
        alert('Invalid JSON: ' + error.message);
    }
}

function copyScript() {
    const script = document.getElementById('browser-script').textContent;
    navigator.clipboard.writeText(script);
    alert('Скрипт скопирован в буфер обмена!');
}

// Copy Trading (Unified)
async function loadUnifiedStatus() {
    try {
        const response = await apiFetch(`${API_URL}/api/copy-trading/status`);

        const data = await response.json();

        if (response.ok) {
            renderUnifiedStatus(data.data);
        }
    } catch (error) {
        console.error('Failed to load unified status:', error);
    }
}

function renderUnifiedStatus(status) {
    const display = document.getElementById('copy-status-display');
    const websocketSettings = document.getElementById('websocket-settings');
    const mirrorSettings = document.getElementById('mirror-settings');

    // Set radio button
    const radio = document.querySelector(`input[name="copy-mode"][value="${status.mode}"]`);
    if (radio) radio.checked = true;

    // Show/hide settings based on mode
    websocketSettings.classList.toggle('hidden', status.mode !== 'websocket');
    mirrorSettings.classList.toggle('hidden', status.mode !== 'mirror');

    // Update status display
    let statusHtml = '';
    const modeLabels = {
        'off': 'Выключено',
        'websocket': 'WebSocket Mode',
        'mirror': 'Browser Mirror'
    };

    statusHtml = `<p><strong>Режим:</strong> ${modeLabels[status.mode] || status.mode}</p>`;

    if (status.mode !== 'off') {
        if (status.master_name) {
            statusHtml += `<p><strong>Master:</strong> ${status.master_name}</p>`;
        }
        statusHtml += `<p><strong>Active Slaves:</strong> ${status.active_slave_count}</p>`;
        if (status.dry_run) {
            statusHtml += `<p class="warning">DRY RUN режим (тестирование)</p>`;
        }
    }

    if (status.mode === 'mirror' && status.mirror_script) {
        document.getElementById('mirror-script-code').textContent = status.mirror_script;
    }

    display.innerHTML = statusHtml;
}

async function handleModeChange(e) {
    const newMode = e.target.value;
    console.log('Mode changed to:', newMode);

    try {
        const ignoreFees = document.getElementById('ignore-fees-checkbox')?.checked || false;

        const response = await apiFetch(`${API_URL}/api/copy-trading/mode`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                mode: newMode,
                ignore_fees: ignoreFees
            })
        });

        if (!response.ok) {
            const data = await response.json();
            alert('Error: ' + (data.error || 'Failed to change mode'));
            loadUnifiedStatus(); // Reload to revert radio
            return;
        }

        loadUnifiedStatus();
    } catch (error) {
        console.error('Failed to change mode:', error);
        loadUnifiedStatus(); // Reload to revert radio
    }
}

// Trades
async function loadTrades() {
    try {
        const response = await apiFetch(`${API_URL}/api/trades?limit=50`);

        const data = await response.json();

        if (response.ok) {
            renderTrades(data.data || []);
        }
    } catch (error) {
        console.error('Failed to load trades:', error);
    }
}

function renderTrades(trades) {
    const container = document.getElementById('trades-list');

    if (trades.length === 0) {
        container.innerHTML = '<p>Нет сделок</p>';
        return;
    }

    container.innerHTML = trades.map(trade => `
        <div class="trade-item">
            <div class="trade-header">
                <div class="trade-symbol">${trade.symbol || '-'} ${trade.action ? `[${trade.action}]` : ''} ${trade.side === 1 ? 'LONG' : trade.side === 2 ? 'SHORT' : ''} ${trade.leverage ? `x${trade.leverage}` : ''}</div>
                <span class="trade-status status-${trade.status}">${trade.status.toUpperCase()}</span>
            </div>
            <div>
                ${trade.volume ? `<strong>Volume:</strong> ${trade.volume} |` : ''}
                <strong>Sent:</strong> ${new Date(trade.sent_at).toLocaleString()}
            </div>
            ${trade.details ? `
                <div style="margin-top: 10px; font-size: 12px;">
                    <strong>Details:</strong> ${trade.details.map(d => `${d.account_name}: ${d.status}`).join(', ')}
                </div>
            ` : ''}
        </div>
    `).join('');
}

// Mirror (copy button handler)
function copyMirrorScript() {
    const script = document.getElementById('mirror-script-code').textContent;
    navigator.clipboard.writeText(script);
    alert('Скрипт скопирован в буфер обмена!');
}

// Logs
async function loadLogs() {
    try {
        const response = await apiFetch(`${API_URL}/api/logs?limit=100`);

        const data = await response.json();

        if (response.ok) {
            renderLogs(data.data || []);
        }
    } catch (error) {
        console.error('Failed to load logs:', error);
    }
}

function renderLogs(logs) {
    const container = document.getElementById('logs-list');

    if (logs.length === 0) {
        container.innerHTML = '<p>Нет логов</p>';
        return;
    }

    container.innerHTML = logs.map(log => `
        <div class="log-item">
            <div style="display: flex; justify-content: space-between; margin-bottom: 5px;">
                <span><strong>[${log.level}]</strong> ${log.action}</span>
                <span style="color: #999; font-size: 12px;">${new Date(log.created_at).toLocaleString()}</span>
            </div>
            <div>${log.message}</div>
        </div>
    `).join('');
}

// === Trades Feed ===

async function loadTradesFeed() {
    try {
        const accountIds = Array.from(selectedAccountIds).join(',');
        const url = accountIds
            ? `${API_URL}/api/trades/feed?account_ids=${accountIds}&limit=10`
            : `${API_URL}/api/trades/feed?limit=10`;

        const response = await apiFetch(url);
        const data = await response.json();

        if (response.ok) {
            renderTradesFeed(data.data || []);
            updateFeedFilterInfo();
        }
    } catch (error) {
        console.error('Failed to load trades feed:', error);
    }
}

function renderTradesFeed(trades) {
    const container = document.getElementById('trades-feed');

    if (!trades || trades.length === 0) {
        container.innerHTML = '<div class="feed-empty">Нет событий</div>';
        return;
    }

    container.innerHTML = trades.map(trade => {
        const sideText = trade.side === 1 ? 'LONG' : trade.side === 2 ? 'SHORT' : '';
        const actionText = trade.action || '';
        const time = new Date(trade.sent_at).toLocaleTimeString();

        let detailsHtml = '';
        if (trade.details && trade.details.length > 0) {
            detailsHtml = `
                <div class="feed-item-details">
                    ${trade.details.map(d => `
                        <div class="feed-detail">
                            <span class="feed-detail-name">${d.account_name || 'Unknown'}</span>
                            <span class="feed-detail-status ${d.status}">${d.status === 'success' ? 'OK' : 'FAIL'}${d.latency_ms ? ` (${d.latency_ms}ms)` : ''}</span>
                        </div>
                    `).join('')}
                </div>
            `;
        }

        return `
            <div class="feed-item">
                <div class="feed-item-header">
                    <span class="feed-item-symbol">${actionText} ${trade.symbol} ${sideText} ${trade.leverage ? `x${trade.leverage}` : ''}</span>
                    <span class="feed-item-time">${time}</span>
                </div>
                <div class="feed-item-master">Master: ${trade.master_account_name || 'Unknown'}</div>
                ${detailsHtml}
            </div>
        `;
    }).join('');
}

function updateFeedFilterInfo() {
    const info = document.getElementById('feed-filter-info');
    if (selectedAccountIds.size === 0) {
        info.textContent = 'Все аккаунты';
    } else {
        info.textContent = `${selectedAccountIds.size} выбрано`;
    }
}

function startFeedAutoRefresh() {
    stopFeedAutoRefresh();
    loadTradesFeed();
    feedInterval = setInterval(loadTradesFeed, 5000);
    console.log('Feed auto-refresh started (5s)');
}

function stopFeedAutoRefresh() {
    if (feedInterval) {
        clearInterval(feedInterval);
        feedInterval = null;
        console.log('Feed auto-refresh stopped');
    }
}

// === Account History Modal ===

async function showAccountHistory(accountId, accountName, isMaster) {
    document.getElementById('account-history-title').textContent = `История: ${accountName}`;
    document.getElementById('account-history-modal').classList.remove('hidden');
    document.getElementById('account-history-list').innerHTML = '<p>Загрузка...</p>';

    try {
        const response = await apiFetch(`${API_URL}/api/accounts/${accountId}/trades?is_master=${isMaster}&limit=50`);
        const data = await response.json();

        if (response.ok) {
            renderAccountHistory(data.data || [], isMaster);
        }
    } catch (error) {
        console.error('Failed to load account history:', error);
        document.getElementById('account-history-list').innerHTML = '<p>Ошибка загрузки</p>';
    }
}

function renderAccountHistory(trades, isMaster) {
    const container = document.getElementById('account-history-list');

    if (!trades || trades.length === 0) {
        container.innerHTML = '<p style="text-align: center; color: #999;">Нет истории</p>';
        return;
    }

    if (isMaster) {
        // Для мастера показываем Trade с деталями
        container.innerHTML = `
            <table class="history-table">
                <thead>
                    <tr>
                        <th>Время</th>
                        <th>Действие</th>
                        <th>Символ</th>
                        <th>Сторона</th>
                        <th>Плечо</th>
                        <th>Статус</th>
                        <th>Исполнено</th>
                    </tr>
                </thead>
                <tbody>
                    ${trades.map(t => {
                        const sideText = t.side === 1 ? 'LONG' : t.side === 2 ? 'SHORT' : '-';
                        const successCount = t.details?.filter(d => d.status === 'success').length || 0;
                        const totalCount = t.details?.length || 0;
                        return `
                            <tr>
                                <td>${new Date(t.sent_at).toLocaleString()}</td>
                                <td>${t.action || '-'}</td>
                                <td>${t.symbol}</td>
                                <td>${sideText}</td>
                                <td>${t.leverage || '-'}x</td>
                                <td><span class="status-badge ${t.status}">${t.status}</span></td>
                                <td>${successCount}/${totalCount}</td>
                            </tr>
                        `;
                    }).join('')}
                </tbody>
            </table>
        `;
    } else {
        // Для slave показываем его детали выполнения
        container.innerHTML = `
            <table class="history-table">
                <thead>
                    <tr>
                        <th>Время</th>
                        <th>Действие</th>
                        <th>Символ</th>
                        <th>Сторона</th>
                        <th>Статус</th>
                        <th>Latency</th>
                        <th>Ошибка</th>
                    </tr>
                </thead>
                <tbody>
                    ${trades.map(t => {
                        const detail = t.details?.[0] || {};
                        const sideText = t.side === 1 ? 'LONG' : t.side === 2 ? 'SHORT' : '-';
                        return `
                            <tr>
                                <td>${new Date(t.sent_at).toLocaleString()}</td>
                                <td>${t.action || '-'}</td>
                                <td>${t.symbol}</td>
                                <td>${sideText}</td>
                                <td><span class="status-badge ${detail.status || t.status}">${detail.status || t.status}</span></td>
                                <td>${detail.latency_ms ? detail.latency_ms + 'ms' : '-'}</td>
                                <td>${detail.error || '-'}</td>
                            </tr>
                        `;
                    }).join('')}
                </tbody>
            </table>
        `;
    }
}

function hideAccountHistoryModal() {
    document.getElementById('account-history-modal').classList.add('hidden');
}
