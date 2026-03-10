const { createApp, ref, computed, onMounted, nextTick } = Vue;

const app = createApp({
    setup() {
        const torrents = ref([]);
        const activities = ref([]);
        const feedStream = ref([]);
        const loading = ref(false);
        const bulkLoading = ref(false);
        const rescoreLoading = ref(false);
        const activeTab = ref('pending');
        const selectedIds = ref(new Set());
        const operatingIds = ref(new Set());
        const openMenuId = ref(null); // ID of card with open kebab menu; null = all closed
        const toasts = ref([]);
        // Initialize dark mode from system preference immediately
        const darkMode = ref(window.matchMedia('(prefers-color-scheme: dark)').matches);
        // Initialize sidebar collapse state from localStorage
        const sidebarCollapsed = ref(JSON.parse(localStorage.getItem('rss-curator-sidebar-collapsed') || 'false'));
        const sidebarTab = ref('activity'); // 'activity' or 'feed'

        // Stats from API (/api/stats) — 24h windowed counts
        const stats = ref({ hours: 24, pending: 0, seen: 0, staged: 0, approved: 0, rejected: 0, queued: 0 });

        // Log drawer state
        const logsDrawerOpen = ref(false);
        const logEntries = ref([]);
        const logFilter = ref('');
        const logLevelFilter = ref(['INFO', 'WARN', 'ERROR', 'DEBUG', 'FATAL']);
        const logAutoScroll = ref(true);
        const logSortDesc = ref(true); // true = newest first (default)
        const logsDrawerHeight = ref('60vh');
        const logsDrawerResizing = ref(false);
        let logEventSource = null;
        
        // Load dark mode preference from localStorage if available, otherwise use system preference
        const savedDarkMode = localStorage.getItem('rss-curator-dark-mode');
        if (savedDarkMode !== null) {
            darkMode.value = JSON.parse(savedDarkMode);
        }
        // Tab structure: pending → accepted → rejected
        // (torrents can be queued for download from accepted tab)
        const tabs = ['pending', 'accepted', 'rejected'];
        const reviewModalOpen = ref(false);
        const reviewingTorrent = ref(null);
        const reviewForm = ref({
            tags: '',
            category: ''
        });
        const bulkReviewModalOpen = ref(false);
        const bulkReviewForm = ref({
            tags: '',
            category: ''
        });
        let toastCounter = 0;

        // Computed properties
        // pendingCount kept for collapsed sidebar badge
        const pendingCount = computed(() =>
            torrents.value.filter(t => t.status === 'pending').length
        );
        const acceptedCount = computed(() =>
            torrents.value.filter(t => t.status === 'accepted').length
        );
        const rejectedCount = computed(() =>
            torrents.value.filter(t => t.status === 'rejected').length
        );
        const selectedCount = computed(() => selectedIds.value.size);
        const multiSelectActive = computed(() => selectedIds.value.size > 1);
        const filteredLogs = computed(() => {
            const filtered = logEntries.value.filter(e => {
                const levelMatch = logLevelFilter.value.includes(e.level);
                const textMatch = !logFilter.value ||
                    e.message.toLowerCase().includes(logFilter.value.toLowerCase());
                return levelMatch && textMatch;
            });
            return logSortDesc.value
                ? filtered.slice().sort((a, b) => b.id - a.id)
                : filtered.slice().sort((a, b) => a.id - b.id);
        });
        const displayedTorrents = computed(() => {
            const filtered = torrents.value.filter(t => t.status === activeTab.value);
            const hasScores = filtered.some(t => t.ai_scored);
            if (hasScores) return filtered.slice().sort((a, b) => (b.ai_score || 0) - (a.ai_score || 0));
            return filtered;
        });
        // Sorted torrents for feed stream (most recent first)
        const feedStreamTorrents = computed(() =>
            torrents.value.slice().sort((a, b) => {
                // Prioritize by newest first (assuming torrents are added in order)
                return b.id - a.id;
            })
        );

        // Methods
        const fetchTorrents = async (status = 'pending') => {
            loading.value = true;
            try {
                const response = await fetch(`/api/torrents?status=${status}`);
                const data = await response.json();
                torrents.value = data.torrents || [];
            } catch (error) {
                console.error('Failed to fetch torrents:', error);
                showToast('Failed to load torrents', 'error');
            } finally {
                loading.value = false;
            }
        };

        const fetchAllTorrents = async () => {
            try {
                const [pending, accepted, rejected] = await Promise.all([
                    fetch('/api/torrents?status=pending').then(r => r.json()),
                    fetch('/api/torrents?status=accepted').then(r => r.json()),
                    fetch('/api/torrents?status=rejected').then(r => r.json())
                ]);
                torrents.value = [
                    ...(pending.torrents || []),
                    ...(accepted.torrents || []),
                    ...(rejected.torrents || [])
                ];
            } catch (error) {
                console.error('Failed to fetch all torrents:', error);
            }
        };

        const fetchActivities = async () => {
            try {
                const response = await fetch(`/api/activity?limit=20&offset=0`);
                const data = await response.json();
                activities.value = data.activities || [];
            } catch (error) {
                console.error('Failed to fetch activities:', error);
            }
        };

        const fetchFeedStream = async () => {
            try {
                const response = await fetch(`/api/feed/stream?limit=50`);
                const data = await response.json();
                feedStream.value = data.items || [];
            } catch (error) {
                console.error('Failed to fetch feed stream:', error);
            }
        };

        const fetchStats = async () => {
            try {
                const response = await fetch('/api/stats');
                const data = await response.json();
                stats.value = { ...stats.value, ...data };
            } catch (error) {
                console.error('Failed to fetch stats:', error);
            }
        };

        const openLogsDrawer = async () => {
            logsDrawerOpen.value = true;
            // Backfill with buffered history
            try {
                const res = await fetch('/api/logs');
                const data = await res.json();
                if (Array.isArray(data)) {
                    logEntries.value = data;
                }
            } catch (e) {
                console.error('Failed to fetch initial logs:', e);
            }
            // Start live SSE stream
            if (logEventSource) logEventSource.close();
            logEventSource = new EventSource('/api/logs/stream');
            logEventSource.onmessage = (event) => {
                try {
                    const entry = JSON.parse(event.data);
                    logEntries.value.push(entry);
                    if (logEntries.value.length > 500) {
                        logEntries.value = logEntries.value.slice(-500);
                    }
                    if (logAutoScroll.value) {
                        nextTick(() => {
                            const el = document.getElementById('log-drawer-body');
                            if (el) el.scrollTop = logSortDesc.value ? 0 : el.scrollHeight;
                        });
                    }
                } catch (e) {
                    // ignore parse errors
                }
            };
            logEventSource.onerror = (e) => {
                console.warn('Log SSE stream error:', e);
            };
        };

        const closeLogsDrawer = () => {
            logsDrawerOpen.value = false;
            if (logEventSource) {
                logEventSource.close();
                logEventSource = null;
            }
        };

        const clearLogs = () => { logEntries.value = []; };

        const toggleLevelFilter = (level) => {
            const idx = logLevelFilter.value.indexOf(level);
            if (idx === -1) {
                logLevelFilter.value.push(level);
            } else if (logLevelFilter.value.length > 1) {
                logLevelFilter.value.splice(idx, 1);
            }
        };

        const startDrawerResize = (e) => {
            e.preventDefault();
            const drawerEl = document.getElementById('log-drawer');
            const startY = e.clientY;
            const startHeight = drawerEl ? drawerEl.offsetHeight : window.innerHeight * 0.6;
            logsDrawerResizing.value = true;
            const onMove = (mv) => {
                const delta = startY - mv.clientY; // drag up = taller
                const clamped = Math.max(80, Math.min(window.innerHeight * 0.92, startHeight + delta));
                logsDrawerHeight.value = clamped + 'px';
            };
            const onUp = () => {
                logsDrawerResizing.value = false;
                document.removeEventListener('mousemove', onMove);
                document.removeEventListener('mouseup', onUp);
            };
            document.addEventListener('mousemove', onMove);
            document.addEventListener('mouseup', onUp);
        };

        const showToast = (message, type = 'info', duration = 5000) => {
            const id = toastCounter++;
            const toast = { id, message, type };
            toasts.value.push(toast);
            setTimeout(() => {
                toasts.value = toasts.value.filter(t => t.id !== id);
            }, duration);
        };

        const approveTorrent = async (id) => {
            // Get the torrent first
            let torrent = torrents.value.find(t => t.id === id);
            if (!torrent) return;
            
            // Send to accepted state (tollgate before download)
            operatingIds.value.add(id);
            try {
                const response = await fetch(`/api/torrents/${id}/approve`, {
                    method: 'POST'
                });
                if (response.ok) {
                    showToast('Torrent accepted! Ready to queue for download.', 'info');
                    await fetchAllTorrents();
                    // Get the updated torrent with new status
                    torrent = torrents.value.find(t => t.id === id);
                    if (torrent) {
                        // Open the queue configuration modal
                        openReviewModal(torrent);
                    }
                } else {
                    showToast('Failed to approve', 'error');
                }
            } catch (error) {
                console.error('Error approving torrent:', error);
                showToast('Error approving torrent', 'error');
            } finally {
                operatingIds.value.delete(id);
            }
        };

        const openReviewModal = (torrent) => {
            reviewingTorrent.value = torrent;
            reviewForm.value = {
                tags: '',
                category: ''
            };
            reviewModalOpen.value = true;
        };

        const closeReviewModal = () => {
            reviewModalOpen.value = false;
            reviewingTorrent.value = null;
        };

        const deferReview = () => {
            // Close modal without taking action - torrent stays in 'accepted' state
            // User can configure and queue later or in bulk
            showToast('Queue deferred. Configure and queue later.', 'info');
            closeReviewModal();
        };

        const submitReview = async () => {
            if (!reviewingTorrent.value) return;
            
            // Store ID before it gets cleared by closeReviewModal
            const torrentId = reviewingTorrent.value.id;
            operatingIds.value.add(torrentId);
            try {
                // Queue the accepted torrent for download
                const response = await fetch(`/api/torrents/${torrentId}/queue`, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        tags: reviewForm.value.tags,
                        category: reviewForm.value.category
                    })
                });
                if (response.ok) {
                    showToast('Queued for download!', 'success');
                    closeReviewModal();
                    await fetchAllTorrents();
                    await fetchActivities();
                } else {
                    showToast('Failed to queue torrent', 'error');
                }
            } catch (error) {
                console.error('Error queueing torrent:', error);
                showToast('Error queueing torrent', 'error');
            } finally {
                operatingIds.value.delete(torrentId);
            }
        };

        const rejectTorrent = async (id) => {
            operatingIds.value.add(id);
            try {
                const response = await fetch(`/api/torrents/${id}/reject`, {
                    method: 'POST'
                });
                if (response.ok) {
                    await response.json();
                    showToast('Torrent rejected.', 'info');
                    await fetchAllTorrents();
                    await fetchActivities();
                } else {
                    showToast('Failed to reject torrent', 'error');
                }
            } catch (error) {
                console.error('Error rejecting torrent:', error);
                showToast('Error rejecting torrent', 'error');
            } finally {
                operatingIds.value.delete(id);
            }
        };

        const queueForDownload = async (id) => {
            // Get the torrent and open the configuration modal
            const torrent = torrents.value.find(t => t.id === id);
            if (!torrent) return;
            openReviewModal(torrent);
        };

        const bulkApprove = async () => {
            if (selectedIds.value.size === 0) return;
            
            bulkLoading.value = true;
            const ids = Array.from(selectedIds.value);
            let successCount = 0;
            
            try {
                for (const id of ids) {
                    const response = await fetch(`/api/torrents/${id}/approve`, {
                        method: 'POST'
                    });
                    if (response.ok) {
                        successCount++;
                    }
                }
                
                if (successCount > 0) {
                    showToast(`Approved ${successCount}/${ids.length} torrents`, 'success');
                    selectedIds.value.clear();
                    await fetchAllTorrents();
                    await fetchActivities();
                }
            } catch (error) {
                console.error('Error in bulk approve:', error);
                showToast('Error approving torrents', 'error');
            } finally {
                bulkLoading.value = false;
            }
        };

        const bulkReject = async () => {
            if (selectedIds.value.size === 0) return;
            
            bulkLoading.value = true;
            const ids = Array.from(selectedIds.value);
            let successCount = 0;
            
            try {
                for (const id of ids) {
                    const response = await fetch(`/api/torrents/${id}/reject`, {
                        method: 'POST'
                    });
                    if (response.ok) {
                        successCount++;
                    }
                }
                
                if (successCount > 0) {
                    showToast(`Rejected ${successCount}/${ids.length} torrents`, 'success');
                    selectedIds.value.clear();
                    await fetchAllTorrents();
                    await fetchActivities();
                }
            } catch (error) {
                console.error('Error in bulk reject:', error);
                showToast('Error rejecting torrents', 'error');
            } finally {
                bulkLoading.value = false;
            }
        };

        const bulkQueue = async () => {
            if (selectedIds.value.size === 0) return;
            
            bulkLoading.value = true;
            try {
                // Queue accepted torrents in bulk without custom config
                // Uses default settings (empty tags, category)
                const results = await Promise.all(
                    Array.from(selectedIds.value).map(id =>
                        fetch(`/api/torrents/${id}/queue`, {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({
                                tags: '',
                                category: ''
                            })
                        })
                    )
                );
                
                if (results.every(r => r.ok)) {
                    showToast(`Queued ${selectedIds.value.size} torrents for download`, 'success');
                    selectedIds.value.clear();
                    await fetchAllTorrents();
                    await fetchActivities();
                } else {
                    showToast('Some queues failed', 'error');
                }
            } catch (error) {
                console.error('Error in bulk queue:', error);
                showToast('Error queueing torrents', 'error');
            } finally {
                bulkLoading.value = false;
            }
        };

        const openBulkReviewModal = () => {
            if (selectedIds.value.size === 0) {
                showToast('No torrents selected', 'error');
                return;
            }
            bulkReviewForm.value = {
                tags: '',
                category: ''
            };
            bulkReviewModalOpen.value = true;
        };

        const closeBulkReviewModal = () => {
            bulkReviewModalOpen.value = false;
        };

        const submitBulkReview = async () => {
            if (selectedIds.value.size === 0) return;
            
            bulkLoading.value = true;
            try {
                // Queue all selected torrents with same config
                const results = await Promise.all(
                    Array.from(selectedIds.value).map(id =>
                        fetch(`/api/torrents/${id}/queue`, {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({
                                tags: bulkReviewForm.value.tags,
                                category: bulkReviewForm.value.category
                            })
                        })
                    )
                );
                
                if (results.every(r => r.ok)) {
                    showToast(`Queued ${selectedIds.value.size} torrents with config`, 'success');
                    selectedIds.value.clear();
                    closeBulkReviewModal();
                    await fetchAllTorrents();
                    await fetchActivities();
                } else {
                    showToast('Some queues failed', 'error');
                }
            } catch (error) {
                console.error('Error in bulk queue:', error);
                showToast('Error queueing torrents', 'error');
            } finally {
                bulkLoading.value = false;
            }
        };

        // toggleCard: unified card-click handler — toggles selection and closes any open menu
        const toggleCard = (id) => {
            openMenuId.value = null;
            if (selectedIds.value.has(id)) {
                selectedIds.value.delete(id);
            } else {
                selectedIds.value.add(id);
            }
        };

        const isSelected = (id) => selectedIds.value.has(id);

        // rescoreOne: single-card re-score from the kebab menu
        const rescoreOne = async (id) => {
            openMenuId.value = null;
            operatingIds.value.add(id);
            try {
                const response = await fetch('/api/torrents/rescore', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ ids: [id] })
                });
                const data = await response.json();
                if (response.ok) {
                    if (Array.isArray(data.torrents)) {
                        data.torrents.forEach(updated => {
                            const idx = torrents.value.findIndex(t => t.id === updated.id);
                            if (idx !== -1) torrents.value[idx] = { ...torrents.value[idx], ...updated };
                        });
                    }
                    showToast('Re-score complete', 'success');
                } else {
                    showToast(data.error || 'Re-score failed', 'error');
                }
            } catch (error) {
                console.error('Error during rescore:', error);
                showToast('Error during re-score', 'error');
            } finally {
                operatingIds.value.delete(id);
            }
        };

        const rescoreSelected = async () => {
            rescoreLoading.value = true;
            const ids = [...selectedIds.value];
            try {
                const response = await fetch('/api/torrents/rescore', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ ids })
                });
                const data = await response.json();
                if (response.ok) {
                    // Merge updated scores back into torrents in-place
                    if (Array.isArray(data.torrents)) {
                        data.torrents.forEach(updated => {
                            const idx = torrents.value.findIndex(t => t.id === updated.id);
                            if (idx !== -1) {
                                torrents.value[idx] = { ...torrents.value[idx], ...updated };
                            }
                        });
                    }
                    showToast(`Re-scored ${data.rescored} torrent${data.rescored !== 1 ? 's' : ''}`, 'success');
                    selectedIds.value = new Set();
                } else {
                    showToast(data.error || 'Re-score failed', 'error');
                }
            } catch (error) {
                console.error('Error during rescore:', error);
                showToast('Error during re-score', 'error');
            } finally {
                rescoreLoading.value = false;
            }
        };

        const formatSize = (bytes) => {
            const units = ['B', 'KB', 'MB', 'GB'];
            let size = bytes;
            let unitIndex = 0;
            while (size >= 1024 && unitIndex < units.length - 1) {
                size /= 1024;
                unitIndex++;
            }
            return `${size.toFixed(2)} ${units[unitIndex]}`;
        };

        // Load initial data
        onMounted(() => {
            // Apply dark mode class immediately based on initial value
            applyDarkMode();
            
            // Listen for system preference changes
            window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (e) => {
                darkMode.value = e.matches;
                applyDarkMode();
            });

            // Close any open kebab menu when clicking outside a card
            document.addEventListener('click', () => { openMenuId.value = null; });
            
            fetchAllTorrents();
            fetchActivities();
            fetchFeedStream();
            fetchStats();
            // Auto-refresh every 30 seconds
            setInterval(() => {
                fetchAllTorrents();
                fetchActivities();
                fetchFeedStream();
                fetchStats();
            }, 30000);
        });

        const toggleDarkMode = () => {
            darkMode.value = !darkMode.value;
            applyDarkMode();
            localStorage.setItem('rss-curator-dark-mode', JSON.stringify(darkMode.value));
        };

        const applyDarkMode = () => {
            const html = document.documentElement;
            if (darkMode.value) {
                html.classList.add('dark');
            } else {
                html.classList.remove('dark');
            }
        };

        const toggleSidebarCollapse = () => {
            sidebarCollapsed.value = !sidebarCollapsed.value;
            localStorage.setItem('rss-curator-sidebar-collapsed', JSON.stringify(sidebarCollapsed.value));
        };

        return {
            torrents,
            activities,
            feedStream,
            stats,
            logsDrawerOpen,
            logEntries,
            logFilter,
            logLevelFilter,
            logAutoScroll,
            logSortDesc,
            logsDrawerHeight,
            logsDrawerResizing,
            startDrawerResize,
            filteredLogs,
            loading,
            bulkLoading,
            rescoreLoading,
            activeTab,
            selectedIds,
            operatingIds,
            openMenuId,
            toasts,
            tabs,
            darkMode,
            sidebarCollapsed,
            sidebarTab,
            feedStreamTorrents,
            pendingCount,
            acceptedCount,
            rejectedCount,
            selectedCount,
            multiSelectActive,
            displayedTorrents,
            fetchTorrents,
            fetchAllTorrents,
            fetchActivities,
            fetchFeedStream,
            fetchStats,
            openLogsDrawer,
            closeLogsDrawer,
            clearLogs,
            toggleLevelFilter,
            approveTorrent,
            rejectTorrent,
            openReviewModal,
            closeReviewModal,
            deferReview,
            submitReview,
            reviewModalOpen,
            reviewingTorrent,
            reviewForm,
            bulkReviewModalOpen,
            bulkReviewForm,
            openBulkReviewModal,
            closeBulkReviewModal,
            submitBulkReview,
            queueForDownload,
            bulkApprove,
            bulkReject,
            bulkQueue,
            toggleCard,
            isSelected,
            rescoreSelected,
            rescoreOne,
            formatSize,
            showToast,
            toggleDarkMode,
            toggleSidebarCollapse
        };
    }
});

app.mount('#app');
