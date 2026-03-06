const { createApp, ref, computed, onMounted } = Vue;

const app = createApp({
    setup() {
        const torrents = ref([]);
        const activities = ref([]);
        const feedStream = ref([]);
        const loading = ref(false);
        const bulkLoading = ref(false);
        const activeTab = ref('pending');
        const selectedIds = ref(new Set());
        const operatingIds = ref(new Set());
        const toasts = ref([]);
        // Initialize dark mode from system preference immediately
        const darkMode = ref(window.matchMedia('(prefers-color-scheme: dark)').matches);
        // Initialize sidebar collapse state from localStorage
        const sidebarCollapsed = ref(JSON.parse(localStorage.getItem('rss-curator-sidebar-collapsed') || 'false'));
        const sidebarTab = ref('activity'); // 'activity' or 'feed'
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
        const displayedTorrents = computed(() => 
            torrents.value.filter(t => t.status === activeTab.value)
        );
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

        const toggleSelection = (id) => {
            if (selectedIds.value.has(id)) {
                selectedIds.value.delete(id);
            } else {
                selectedIds.value.add(id);
            }
        };

        const isSelected = (id) => selectedIds.value.has(id);

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
            
            fetchAllTorrents();
            fetchActivities();
            fetchFeedStream();
            // Auto-refresh every 30 seconds
            setInterval(() => {
                fetchAllTorrents();
                fetchActivities();
                fetchFeedStream();
            }, 30000);
        });

        const toggleDarkMode = () => {
            darkMode.value = !darkMode.value;
            applyDarkMode();
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
            loading,
            bulkLoading,
            activeTab,
            selectedIds,
            operatingIds,
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
            displayedTorrents,
            fetchTorrents,
            fetchAllTorrents,
            fetchActivities,
            fetchFeedStream,
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
            toggleSelection,
            isSelected,
            formatSize,
            showToast,
            toggleDarkMode,
            toggleSidebarCollapse
        };
    }
});

app.mount('#app');
