const { createApp, ref, computed, watch, onMounted, nextTick } = Vue;

const app = createApp({
    setup() {
        const torrents = ref([]);
        const activities = ref([]);
        const feedStream = ref([]);
        const loading = ref(false);
        const bulkLoading = ref(false);
        const rescoreLoading = ref(false);
        const rematchLoading = ref(false);
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

        // Jobs state
        const jobs = ref([]);
        const jobsPopoverOpen = ref(false);
        const activeJobIds = ref(new Map()); // job_id → { type, label } for in-flight jobs started from this session
        let jobsEventSource = null;

        // Bulk-actions dropdown state (Phase B)
        const actionsDropdownOpen = ref(false);

        // Alerts state
        const alerts = ref([]);
        const alertsPopoverOpen = ref(false);
        const lastReadAt = ref(new Date(localStorage.getItem('rss-curator-alerts-read-at') || 0));
        let alertsEventSource = null;

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
        const rematchModalOpen = ref(false);
        const rematchIds = ref([]);
        const rematchAutoRescore = ref(true);
        const rematchForceAIEnrich = ref(false);
        let toastCounter = 0;

        // Sort + pagination state
        const sortField = ref('staged_at'); // 'title' | 'staged_at' | 'pub_date' | 'size' | 'ai_score'
        const sortDir   = ref('desc');      // 'asc' | 'desc'
        const pageSize  = ref(25);          // items per page; 0 = all
        const currentPage = ref(1);

        // Sort field display labels
        const sortFields = [
            { key: 'staged_at', label: 'Date Staged' },
            { key: 'pub_date',  label: 'Pub Date' },
            { key: 'title',     label: 'Title' },
            { key: 'size',      label: 'Size' },
            { key: 'ai_score',  label: 'AI Score' },
        ];
        const pageSizeOptions = [25, 50, 100, 0]; // 0 = all

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
        // Jobs computed
        const runningJobs = computed(() => jobs.value.filter(j => j.status === 'running'));
        const failedJobs  = computed(() => jobs.value.filter(j => j.status === 'failed'));
        const cancelledJobs = computed(() => jobs.value.filter(j => j.status === 'cancelled'));
        const completedJobs = computed(() => jobs.value.filter(j => j.status === 'completed'));
        const recentJobs  = computed(() => jobs.value.slice(0, 5));
        const latestTerminalJob = computed(() => jobs.value.find(j => j.status !== 'running') || null);
        const railRunningJobs = computed(() => runningJobs.value.slice(0, 3));
        const jobsRailVisible = computed(() =>
            runningJobs.value.length > 0 ||
            failedJobs.value.length > 0 ||
            cancelledJobs.value.length > 0 ||
            latestTerminalJob.value !== null
        );
        // Batch progress tracking — counts jobs in the current/latest session
        // (jobs started within ~1s of the latest started_at time).
        const batchStats = computed(() => {
            const batchableTypes = ['rematch', 'rescore'];
            const allBatchJobs = jobs.value.filter(j => batchableTypes.includes(j.type));
            if (allBatchJobs.length === 0) return { total: 0, completed: 0, running: 0 };
            const latestStartTime = new Date(allBatchJobs[0].started_at).getTime();
            const batchWindow = 2000; // 2s window to detect batch boundaries
            const batchJobs = allBatchJobs.filter(j =>
                Math.abs(new Date(j.started_at).getTime() - latestStartTime) <= batchWindow
            );
            const running = batchJobs.filter(j => j.status === 'running').length;
            const completed = batchJobs.filter(j => j.status === 'completed').length;
            return { total: batchJobs.length, running, completed };
        });
        // Persistent list of running UI-relevant jobs for the torrent-view strip.
        // This derives from live SSE-backed jobs state so it survives refresh/navigation.
        const activeJobList = computed(() =>
            runningJobs.value
                .filter(job => job.type === 'rematch' || job.type === 'rescore')
                .map(job => ({
                    id: job.id,
                    type: job.type,
                    label: job.type === 'rematch' ? 'Re-match' : 'Re-score',
                    progress: job.progress || null,
                }))
        );

        // Alerts computed
        const unreadAlerts = computed(() =>
            alerts.value.filter(a => new Date(a.triggered_at) > lastReadAt.value)
        );
        const recentAlerts = computed(() => alerts.value.slice().reverse().slice(0, 5));

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
            const dir = sortDir.value === 'asc' ? 1 : -1;
            return filtered.slice().sort((a, b) => {
                const af = a.feed_item || {};
                const bf = b.feed_item || {};
                switch (sortField.value) {
                    case 'title':
                        return dir * (af.title || '').localeCompare(bf.title || '');
                    case 'pub_date':
                        return dir * (new Date(af.pub_date || 0) - new Date(bf.pub_date || 0));
                    case 'size':
                        return dir * ((af.size || 0) - (bf.size || 0));
                    case 'ai_score':
                        return dir * ((a.ai_score || 0) - (b.ai_score || 0));
                    case 'staged_at':
                    default:
                        return dir * (new Date(a.staged_at || 0) - new Date(b.staged_at || 0));
                }
            });
        });
        const totalPages = computed(() => {
            if (!pageSize.value) return 1;
            return Math.max(1, Math.ceil(displayedTorrents.value.length / pageSize.value));
        });
        const pagedTorrents = computed(() => {
            if (!pageSize.value) return displayedTorrents.value;
            const start = (currentPage.value - 1) * pageSize.value;
            return displayedTorrents.value.slice(start, start + pageSize.value);
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

        const openRematchModal = (ids) => {
            if (!Array.isArray(ids) || ids.length === 0) {
                showToast('No torrents selected for rematch', 'error');
                return;
            }
            rematchIds.value = [...ids];
            rematchAutoRescore.value = true;
            rematchForceAIEnrich.value = false;
            rematchModalOpen.value = true;
        };

        const closeRematchModal = () => {
            rematchModalOpen.value = false;
            rematchIds.value = [];
            rematchForceAIEnrich.value = false;
        };

        const rematchOne = (id) => {
            openMenuId.value = null;
            openRematchModal([id]);
        };

        const rematchSelected = () => {
            const ids = [...selectedIds.value];
            openRematchModal(ids);
        };

        const selectAll = () => {
            pagedTorrents.value.forEach(t => selectedIds.value.add(t.id));
        };

        const submitRematch = async () => {
            if (rematchIds.value.length === 0) return;

            rematchLoading.value = true;
            try {
                const response = await fetch('/api/torrents/rematch', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        ids: rematchIds.value,
                        auto_rescore: rematchAutoRescore.value,
                        force_ai_enrich: rematchForceAIEnrich.value
                    })
                });
                const data = await response.json();
                if (!response.ok) {
                    showToast(data.error || 'Re-match failed', 'error');
                } else if (response.status === 202) {
                    activeJobIds.value = new Map(activeJobIds.value).set(data.job_id, { type: 'rematch', label: 'Re-match' });
                    showToast(`Re-match queued (job #${data.job_id})`, 'info');
                    selectedIds.value = new Set();
                    closeRematchModal();
                } else {
                    if (Array.isArray(data.torrents)) {
                        data.torrents.forEach(updated => {
                            const idx = torrents.value.findIndex(t => t.id === updated.id);
                            if (idx !== -1) torrents.value[idx] = { ...torrents.value[idx], ...updated };
                        });
                    }

                    let message = `Re-match complete: ${data.rematched} matched, ${data.no_longer_matches} cleaned`;
                    if ((data.rescored || 0) > 0) message += `, ${data.rescored} re-scored`;
                    if ((data.skipped || 0) > 0) message += `, ${data.skipped} skipped`;
                    showToast(message, 'success');

                    selectedIds.value = new Set();
                    closeRematchModal();
                }
            } catch (error) {
                console.error('Error during rematch:', error);
                showToast('Error during re-match', 'error');
            } finally {
                rematchLoading.value = false;
            }
        };

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
                if (!response.ok) {
                    showToast(data.error || 'Re-score failed', 'error');
                } else if (response.status === 202) {
                    activeJobIds.value = new Map(activeJobIds.value).set(data.job_id, { type: 'rescore', label: 'Re-score' });
                    showToast(`Re-score queued (job #${data.job_id})`, 'info');
                } else {
                    if (Array.isArray(data.torrents)) {
                        data.torrents.forEach(updated => {
                            const idx = torrents.value.findIndex(t => t.id === updated.id);
                            if (idx !== -1) torrents.value[idx] = { ...torrents.value[idx], ...updated };
                        });
                    }
                    showToast('Re-score complete', 'success');
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
                if (!response.ok) {
                    showToast(data.error || 'Re-score failed', 'error');
                } else if (response.status === 202) {
                    activeJobIds.value = new Map(activeJobIds.value).set(data.job_id, { type: 'rescore', label: 'Re-score' });
                    showToast(`Re-score queued (job #${data.job_id})`, 'info');
                    selectedIds.value = new Set();
                } else {
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

        // Clear selection and open menu when navigating between tabs
        watch(activeTab, () => {
            selectedIds.value = new Set();
            openMenuId.value = null;
            currentPage.value = 1;
        });

        // Reset to page 1 when sort changes
        watch([sortField, sortDir, pageSize], () => { currentPage.value = 1; });

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
            document.addEventListener('click', () => {
                openMenuId.value = null;
                actionsDropdownOpen.value = false;
            });
            
            fetchAllTorrents();
            fetchActivities();
            fetchFeedStream();
            fetchStats();
            fetchJobs();
            openJobsStream();
            fetchAlerts();
            openAlertsStream();
            // Auto-refresh every 30 seconds
            setInterval(() => {
                fetchAllTorrents();
                fetchActivities();
                fetchFeedStream();
                fetchStats();
            }, 30000);
        });

        // Jobs helpers
        const fetchJobs = async () => {
            try {
                const res = await fetch('/api/jobs?limit=20');
                if (!res.ok) return;
                const data = await res.json();
                jobs.value = data || [];
            } catch (e) {
                // silently ignore — jobs UI is non-critical
            }
        };

        // Called when an SSE event closes a job that this session started.
        // Shows a summary toast and refreshes the torrent list.
        const onJobResolved = (job) => {
            const info = activeJobIds.value.get(job.id);
            if (!info) return;
            const next = new Map(activeJobIds.value);
            next.delete(job.id);
            activeJobIds.value = next;
            if (job.status === 'failed') {
                const msg = job.summary?.error_message || `${info.label} job #${job.id} failed`;
                showToast(msg, 'error');
            } else {
                const s = job.summary || {};
                const parts = [];
                if (s.items_matched != null) parts.push(`${s.items_matched} matched`);
                if (s.items_scored  != null) parts.push(`${s.items_scored} scored`);
                if (s.items_queued  != null && s.items_queued > 0) parts.push(`${s.items_queued} queued`);
                const detail = parts.length ? ` — ${parts.join(', ')}` : '';
                showToast(`${info.label} complete${detail}`, 'success');
            }
            fetchAllTorrents();
        };

        const dismissJob = (id) => {
            const next = new Map(activeJobIds.value);
            next.delete(id);
            activeJobIds.value = next;
        };

        const openJobsStream = () => {
            if (jobsEventSource) return; // already open
            jobsEventSource = new EventSource('/api/jobs/stream');
            jobsEventSource.onmessage = (e) => {
                try {
                    const job = JSON.parse(e.data);
                    const idx = jobs.value.findIndex(j => j.id === job.id);
                    if (idx >= 0) {
                        jobs.value.splice(idx, 1, job);
                    } else {
                        jobs.value.unshift(job);
                    }
                    // Keep list bounded
                    if (jobs.value.length > 100) jobs.value.splice(100);
                    // Resolve any job this session started
                    if (activeJobIds.value.has(job.id) && (job.status === 'completed' || job.status === 'failed')) {
                        onJobResolved(job);
                    }
                } catch (_) {}
            };
            jobsEventSource.onerror = () => {
                jobsEventSource.close();
                jobsEventSource = null;
                // Reconnect after 5 seconds
                setTimeout(openJobsStream, 5000);
            };
        };

        const formatRelative = (isoStr) => {
            if (!isoStr) return '';
            const diff = Date.now() - new Date(isoStr).getTime();
            if (diff < 60000)  return 'just now';
            if (diff < 3600000) return Math.floor(diff / 60000) + 'm ago';
            if (diff < 86400000) return Math.floor(diff / 3600000) + 'h ago';
            return Math.floor(diff / 86400000) + 'd ago';
        };

        const jobSummaryLine = (job) => {
            if (!job) return '—';
            if (job.status === 'running') return job.progress || 'running…';
            if (job.status === 'failed') return job.summary?.error_message || 'failed';
            if (job.status === 'cancelled') {
                const parts = [];
                if ((job.summary?.items_matched || 0) > 0) parts.push(`${job.summary.items_matched} matched`);
                if ((job.summary?.items_scored || 0) > 0) parts.push(`${job.summary.items_scored} scored`);
                return parts.length ? `cancelled — ${parts.join(' · ')}` : 'cancelled';
            }
            const parts = [];
            if ((job.summary?.items_found || 0) > 0) parts.push(`${job.summary.items_found} found`);
            if ((job.summary?.items_matched || 0) > 0) parts.push(`${job.summary.items_matched} matched`);
            if ((job.summary?.items_scored || 0) > 0) parts.push(`${job.summary.items_scored} scored`);
            if ((job.summary?.items_queued || 0) > 0) parts.push(`${job.summary.items_queued} queued`);
            return parts.length ? parts.join(' · ') : 'completed';
        };

        // Alerts helpers
        const fetchAlerts = async () => {
            try {
                const res = await fetch('/api/alerts');
                if (!res.ok) return;
                const data = await res.json();
                alerts.value = data || [];
            } catch (_) {}
        };

        const openAlertsStream = () => {
            if (alertsEventSource) return;
            alertsEventSource = new EventSource('/api/alerts/stream');
            alertsEventSource.onmessage = (e) => {
                try {
                    const alert = JSON.parse(e.data);
                    // Dismissed alerts are removed from local state.
                    if (alert.dismissed) {
                        alerts.value = alerts.value.filter(a => a.id !== alert.id);
                        return;
                    }
                    const idx = alerts.value.findIndex(a => a.id === alert.id);
                    if (idx >= 0) {
                        alerts.value.splice(idx, 1, alert);
                    } else {
                        alerts.value.push(alert);
                        // Auto-clear transient (success/neutral) alerts after 3s.
                        const autoClearActions = ['approve', 'reject', 'queue', 'staged', 'job_completed', 'job_cancelled'];
                        if (autoClearActions.includes(alert.action)) {
                            setTimeout(() => dismissAlert(alert.id), 3000);
                        }
                    }
                    if (alerts.value.length > 50) alerts.value.shift();
                } catch (_) {}
            };
            alertsEventSource.onerror = () => {
                alertsEventSource.close();
                alertsEventSource = null;
                setTimeout(openAlertsStream, 5000);
            };
        };

        const markAlertsRead = () => {
            lastReadAt.value = new Date();
            localStorage.setItem('rss-curator-alerts-read-at', lastReadAt.value.toISOString());
        };

        const clearAlerts = () => {
            alerts.value = [];
            markAlertsRead();
        };

        const dismissAlert = async (id) => {
            // Optimistically remove from local state, then sync with server.
            alerts.value = alerts.value.filter(a => a.id !== id);
            try {
                await fetch(`/api/alerts/dismiss/${id}`, { method: 'POST' });
            } catch (_) {}
        };

        // Job cancellation — calls POST /api/jobs/{id}/cancel
        const cancelingJobIds = new Set();
        const cancelJob = async (id) => {
            if (cancelingJobIds.has(id)) return Promise.resolve();
            cancelingJobIds.add(id);
            try {
                const res = await fetch(`/api/jobs/${id}/cancel`, { method: 'POST' });
                if (!res.ok) cancelingJobIds.delete(id);
            } catch (_) {
                cancelingJobIds.delete(id);
            }
        };

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
            rematchLoading,
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
            pagedTorrents,
            totalPages,
            sortField,
            sortDir,
            sortFields,
            pageSize,
            pageSizeOptions,
            currentPage,
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
            rematchModalOpen,
            rematchIds,
            rematchAutoRescore,
            rematchForceAIEnrich,
            openRematchModal,
            closeRematchModal,
            submitRematch,
            queueForDownload,
            bulkApprove,
            bulkReject,
            bulkQueue,
            toggleCard,
            isSelected,
            rematchSelected,
            rematchOne,
            rescoreSelected,
            rescoreOne,
            formatSize,
            showToast,
            toggleDarkMode,
            toggleSidebarCollapse,
            jobs,
            jobsPopoverOpen,
            activeJobIds,
            activeJobList,
            actionsDropdownOpen,
            dismissJob,
            runningJobs,
            failedJobs,
            cancelledJobs,
            recentJobs,
            latestTerminalJob,
            railRunningJobs,
            jobsRailVisible,
            batchStats,
            fetchJobs,
            formatRelative,
            jobSummaryLine,
            selectAll,
            alerts,
            alertsPopoverOpen,
            unreadAlerts,
            recentAlerts,
            markAlertsRead,
            clearAlerts,
            dismissAlert,
            cancelJob,
        };
    }
});

if (window.registerJobsRailComponent) {
    window.registerJobsRailComponent(app);
}
if (window.registerOpsBannerComponent) {
    window.registerOpsBannerComponent(app);
}

app.mount('#app');
