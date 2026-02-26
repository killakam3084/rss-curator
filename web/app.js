const { createApp, ref, computed, onMounted } = Vue;

const app = createApp({
    setup() {
        const torrents = ref([]);
        const loading = ref(false);
        const activeTab = ref('pending');
        const selectedIds = ref(new Set());
        const tabs = ['pending', 'approved', 'rejected'];

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
            } finally {
                loading.value = false;
            }
        };

        const approveTorrent = async (id) => {
            try {
                const response = await fetch(`/api/torrents/${id}/approve`, {
                    method: 'POST'
                });
                if (response.ok) {
                    await fetchTorrents('pending');
                    await fetchTorrents('approved');
                } else {
                    alert('Failed to approve torrent');
                }
            } catch (error) {
                console.error('Error approving torrent:', error);
            }
        };

        const rejectTorrent = async (id) => {
            try {
                const response = await fetch(`/api/torrents/${id}/reject`, {
                    method: 'POST'
                });
                if (response.ok) {
                    await fetchTorrents('pending');
                    await fetchTorrents('rejected');
                } else {
                    alert('Failed to reject torrent');
                }
            } catch (error) {
                console.error('Error rejecting torrent:', error);
            }
        };

        const bulkApprove = async () => {
            for (const id of selectedIds.value) {
                await approveTorrent(id);
            }
            selectedIds.value.clear();
        };

        const bulkReject = async () => {
            for (const id of selectedIds.value) {
                await rejectTorrent(id);
            }
            selectedIds.value.clear();
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
            activeTab,
            selectedIds,
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
            formatSize
        };
    }
});

app.mount('#app');
