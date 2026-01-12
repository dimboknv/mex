// API Configuration (loaded from /config.js)
const API_URL = window.APP_CONFIG?.API_URL || '';

// State
let token = localStorage.getItem('token');
let username = localStorage.getItem('username');
let currentPage = 'accounts';
let balancesInterval = null;

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
            username = data.data.username;
            localStorage.setItem('token', token);
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
            username = data.data.username;
            localStorage.setItem('token', token);
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
    localStorage.removeItem('token');
    localStorage.removeItem('username');
    token = null;
    username = null;
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

    // Stop balances auto-refresh when leaving accounts page
    if (page !== 'accounts') {
        stopBalancesAutoRefresh();
    }

    // Load data for page
    if (page === 'accounts') {
        loadAccounts();
        startBalancesAutoRefresh();
    }
    if (page === 'copytrading') loadUnifiedStatus();
    if (page === 'trades') loadTrades();
    if (page === 'logs') loadLogs();
}

// Accounts
async function loadAccounts(withDetails = false) {
    try {
        const endpoint = withDetails ? '/api/accounts/details' : '/api/accounts';
        const response = await fetch(`${API_URL}${endpoint}`, {
            headers: { 'Authorization': `Bearer ${token}` }
        });

        const data = await response.json();

        if (response.ok) {
            renderAccounts(data.data || [], withDetails);
        } else if (response.status === 401) {
            handleLogout();
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
        <div class="account-card ${acc.is_master ? 'master' : ''} ${acc.disabled ? 'disabled' : ''}">
            <div class="account-header">
                <div class="account-name">${acc.name}</div>
                <div>
                    ${acc.is_master ? '<span class="account-badge badge-master">👑 Master</span>' : ''}
                    ${acc.disabled ? '<span class="account-badge badge-disabled">⏸️ Disabled</span>' : '<span class="account-badge badge-enabled">✅ Active</span>'}
                    ${withDetails && (acc.maker_fee > 0 || acc.taker_fee > 0) ? '<span class="account-badge badge-fee">⚠️ Fee</span>' : ''}
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
                ${!acc.is_master ? `<button class="btn-primary btn-small" onclick="setMaster(${acc.id})">Set Master</button>` : ''}
                <button class="btn-${acc.disabled ? 'success' : 'secondary'} btn-small" onclick="toggleDisabled(${acc.id}, ${!acc.disabled})">${acc.disabled ? 'Enable' : 'Disable'}</button>
                <button class="btn-danger btn-small" onclick="deleteAccount(${acc.id})">Delete</button>
            </div>
        </div>
    `).join('');
}

async function setMaster(accountId) {
    try {
        const response = await fetch(`${API_URL}/api/accounts/${accountId}/master`, {
            method: 'PUT',
            headers: { 'Authorization': `Bearer ${token}` }
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
        const response = await fetch(`${API_URL}/api/accounts/${accountId}/disabled`, {
            method: 'PUT',
            headers: {
                'Authorization': `Bearer ${token}`,
                'Content-Type': 'application/json'
            },
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
        const response = await fetch(`${API_URL}/api/accounts/${accountId}`, {
            method: 'DELETE',
            headers: { 'Authorization': `Bearer ${token}` }
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
    const response = await fetch(`${API_URL}/api/accounts/script`, {
        headers: { 'Authorization': `Bearer ${token}` }
    });
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

        const response = await fetch(`${API_URL}/api/accounts`, {
            method: 'POST',
            headers: {
                'Authorization': `Bearer ${token}`,
                'Content-Type': 'application/json'
            },
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
        const response = await fetch(`${API_URL}/api/copy-trading/unified-status`, {
            headers: { 'Authorization': `Bearer ${token}` }
        });

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
        if (newMode === 'off') {
            // Stop both modes
            await fetch(`${API_URL}/api/copy-trading/stop`, {
                method: 'POST',
                headers: { 'Authorization': `Bearer ${token}` }
            });
            await fetch(`${API_URL}/api/mirror/stop`, {
                method: 'POST',
                headers: { 'Authorization': `Bearer ${token}` }
            });
        } else if (newMode === 'websocket') {
            const ignoreFees = document.getElementById('ignore-fees-checkbox').checked;
            const response = await fetch(`${API_URL}/api/copy-trading/start`, {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${token}`,
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({ ignore_fees: ignoreFees })
            });

            if (!response.ok) {
                const data = await response.json();
                alert('Error: ' + (data.error || 'Failed to start WebSocket mode'));
                loadUnifiedStatus(); // Reload to revert radio
                return;
            }
        } else if (newMode === 'mirror') {
            const response = await fetch(`${API_URL}/api/mirror/start`, {
                method: 'POST',
                headers: { 'Authorization': `Bearer ${token}` }
            });

            if (!response.ok) {
                const data = await response.json();
                alert('Error: ' + (data.error || 'Failed to start Mirror mode'));
                loadUnifiedStatus(); // Reload to revert radio
                return;
            }
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
        const response = await fetch(`${API_URL}/api/trades?limit=50`, {
            headers: { 'Authorization': `Bearer ${token}` }
        });

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
                <div class="trade-symbol">${trade.symbol} ${trade.side === 1 ? 'LONG' : 'SHORT'} x${trade.leverage}</div>
                <span class="trade-status status-${trade.status}">${trade.status.toUpperCase()}</span>
            </div>
            <div>
                <strong>Volume:</strong> ${trade.volume} |
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
        const response = await fetch(`${API_URL}/api/logs?limit=100`, {
            headers: { 'Authorization': `Bearer ${token}` }
        });

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
