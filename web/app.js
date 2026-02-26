// Auto-refresh settings
let autoRefreshEnabled = false;
let autoRefreshInterval = null;
const AUTO_REFRESH_INTERVAL = 30000; // 30 seconds

// Initialize on page load
document.addEventListener('DOMContentLoaded', () => {
    loadInitialData();
    setupEventListeners();
});

function setupEventListeners() {
    document.getElementById('refreshBtn')?.addEventListener('click', refreshData);
    document.getElementById('autoRefresh')?.addEventListener('change', toggleAutoRefresh);
}

function toggleAutoRefresh(e) {
    autoRefreshEnabled = e.target.checked;
    
    if (autoRefreshEnabled) {
        refreshData();
        autoRefreshInterval = setInterval(refreshData, AUTO_REFRESH_INTERVAL);
    } else {
        if (autoRefreshInterval) {
            clearInterval(autoRefreshInterval);
            autoRefreshInterval = null;
        }
    }
}

async function loadInitialData() {
    try {
        await updateStats();
        await fetchAndDisplayTorrents();
    } catch (error) {
        console.error('Failed to load initial data:', error);
        showError('Failed to load dashboard');
    }
}

async function refreshData() {
    try {
        const btn = document.getElementById('refreshBtn');
        if (btn) {
            btn.classList.add('refreshing');
            btn.disabled = true;
        }
        
        await updateStats();
        await fetchAndDisplayTorrents();
        updateLastRefreshed();
    } catch (error) {
        console.error('Refresh failed:', error);
        showError('Failed to refresh data');
    } finally {
        const btn = document.getElementById('refreshBtn');
        if (btn) {
            btn.classList.remove('refreshing');
            btn.disabled = false;
        }
    }
}

async function updateStats() {
    try {
        const response = await fetch('/api/torrents');
        if (!response.ok) throw new Error('Failed to fetch stats');
        
        const data = await response.json();
        const torrents = data.torrents || [];
        
        const pending = torrents.filter(t => t.status === 'pending').length;
        const approved = torrents.filter(t => t.status === 'approved').length;
        const rejected = torrents.filter(t => t.status === 'rejected').length;
        
        updateStat('pendingCount', pending);
        updateStat('approvedCount', approved);
        updateStat('rejectedCount', rejected);
        
        // Check health
        const healthResponse = await fetch('/api/health');
        const healthData = await healthResponse.json();
        updateStat('healthStatus', healthData.status === 'ok' ? '✓ Healthy' : '✗ Issues');
        
    } catch (error) {
        console.error('Failed to update stats:', error);
    }
}

function updateStat(elementId, value) {
    const element = document.getElementById(elementId);
    if (element) {
        element.textContent = value;
    }
}

async function fetchAndDisplayTorrents() {
    try {
        const statusFilter = new URLSearchParams(window.location.search).get('status') || 'pending';
        const url = `/api/torrents${statusFilter ? '?status=' + statusFilter : ''}`;
        
        const response = await fetch(url);
        if (!response.ok) throw new Error('Failed to fetch torrents');
        
        const data = await response.json();
        const torrents = (data.torrents || []).filter(t => t.status === 'pending');
        
        displayTorrents(torrents);
    } catch (error) {
        console.error('Failed to fetch torrents:', error);
        showError('Failed to load torrents');
    }
}

function displayTorrents(torrents) {
    const container = document.getElementById('torrents-list');
    if (!container) return;
    
    if (torrents.length === 0) {
        container.innerHTML = `
            <div class="empty">
                <div class="empty-icon">✓</div>
                <div>No pending torrents</div>
            </div>
        `;
        return;
    }
    
    container.innerHTML = torrents.map(torrent => `
        <div class="torrent-card">
            <div class="torrent-title">${escapeHtml(torrent.title)}</div>
            <div class="torrent-meta">
                <div class="meta-item">
                    <span class="meta-label">Size</span>
                    <span class="meta-value size-value">${formatSize(torrent.size)}</span>
                </div>
                <div class="meta-item">
                    <span class="meta-label">Match</span>
                    <span class="meta-value">${escapeHtml(torrent.match_reason)}</span>
                </div>
            </div>
            <div class="reason">📝 Reason: ${escapeHtml(torrent.match_reason)}</div>
            <div class="torrent-actions">
                <button class="btn btn-approve" onclick="approve('${torrent.id}')">
                    ✓ Approve
                </button>
                <button class="btn btn-reject" onclick="reject('${torrent.id}')">
                    ✗ Reject
                </button>
            </div>
        </div>
    `).join('');
}

async function approve(torrentId) {
    if (!confirm('Approve this torrent?')) return;
    
    try {
        const response = await fetch(`/api/torrents/${torrentId}/approve`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({})
        });
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to approve');
        }
        
        showSuccess('Torrent approved!');
        await refreshData();
    } catch (error) {
        console.error('Approve failed:', error);
        showError('Failed to approve torrent: ' + error.message);
    }
}

async function reject(torrentId) {
    if (!confirm('Reject this torrent?')) return;
    
    try {
        const response = await fetch(`/api/torrents/${torrentId}/reject`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({})
        });
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to reject');
        }
        
        showSuccess('Torrent rejected!');
        await refreshData();
    } catch (error) {
        console.error('Reject failed:', error);
        showError('Failed to reject torrent: ' + error.message);
    }
}

function updateLastRefreshed() {
    const element = document.querySelector('.last-updated');
    if (element) {
        const now = new Date();
        element.textContent = `Last updated: ${now.toLocaleTimeString()}`;
    }
}

function showSuccess(message) {
    showStatusMessage(message, 'status-success');
}

function showError(message) {
    showStatusMessage(message, 'status-error');
}

function showStatusMessage(message, className) {
    const container = document.getElementById('torrents-list');
    if (!container) return;
    
    const messageEl = document.createElement('div');
    messageEl.className = `status-message ${className}`;
    messageEl.textContent = message;
    
    container.insertBefore(messageEl, container.firstChild);
    
    setTimeout(() => messageEl.remove(), 4000);
}

function formatSize(bytes) {
    if (!bytes || bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i];
}

function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}
