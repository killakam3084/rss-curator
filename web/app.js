const { createApp, ref, computed, onMounted } = Vue;

const app = createApp({
    setup() {
        const torrents = ref([]);
        const loading = ref(false);
        const bulkLoading = ref(false);
        const activeTab = ref('pending');
        const selectedIds = ref(new Set());
        const operatingIds = ref(new Set());
        const toasts = ref([]);
        const tabs = ['pending', 'approved', 'rejected'];
        let toastCounter = 0;

        // Computed properties
        const pendingCount = computed(() => 
            torrents.value.filter(t => t.status === 'pending').length
        );
        const approvedCount = computed(() => 
            torrents.value.filter(t => t.status === 'approved').length
        );
        const rejectedCount = computed(() => 
            torrents.value.filter(t => t.status === 'rejected').length
        );
        const selectedCount = computed(() => selectedIds.value.size);
        const displayedTorrents = computed(() => 
            torrents.value.filter(t => t.status === activeTab.value)
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

        const showToast = (message, type = 'info', duration = 5000) => {
            const id = toastCounter++;
            const toast = { id, message, type };
            toasts.value.push(toast);
            setTimeout(() => {
                toasts.value = toasts.value.filter(t => t.id !== id);
            }, duration);
        };

        const approveTorrent = async (id) => {
            operatingIds.value.add(id);
            try {
                const response = await fetch(`/api/torrents/${id}/approve`, {
                    method: 'POST'
                });
                if (response.ok) {
                    showToast('Torrent approved!', 'success');
                    await fetchTorrents('pending');
                    await fetchTorrents('approved');
                } else {
                    showToast('Failed to approve torrent', 'error');
                }
            } catch (error) {
                console.error('Error approving torrent:', error);
                showToast('Error approving torrent', 'error');
            } finally {
                operatingIds.value.delete(id);
            }
        };

        const rejectTorrent = async (id) => {
            operatingIds.value.add(id);
            try {
                const response = await fetch(`/api/torrents/${id}/reject`, {
                    method: 'POST'
                });
                if (response.ok) {
                    showToast('Torrent rejected!', 'success');
                    await fetchTorrents('pending');
                    await fetchTorrents('rejected');
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
                    await fetchTorrents('pending');
                    await fetchTorrents('approved');
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
                    await fetchTorrents('pending');
                    await fetchTorrents('rejected');
                }
            } catch (error) {
                console.error('Error in bulk reject:', error);
                showToast('Error rejecting torrents', 'error');
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
            fetchTorrents('pending');
            // Auto-refresh every 30 seconds
            setInterval(() => {
                fetchTorrents(activeTab.value);
            }, 30000);
        });

        return {
            torrents,
            loading,
            bulkLoading,
            activeTab,
            selectedIds,
            operatingIds,
            toasts,
            tabs,
            pendingCount,
            approvedCount,
            rejectedCount,
            selectedCount,
            displayedTorrents,
            fetchTorrents,
            approveTorrent,
            rejectTorrent,
            bulkApprove,
            bulkReject,
            toggleSelection,
            isSelected,
            formatSize,
            showToast
        };
    }
});

app.mount('#app');
