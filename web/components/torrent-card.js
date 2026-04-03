(function (global) {
    function registerTorrentCardComponent(app) {
        const { ref, onMounted, onUnmounted } = Vue;

        app.component('torrent-card', {
            props: {
                torrent:          { type: Object,  required: true },
                selected:         { type: Boolean, default: false },
                multiSelectActive:{ type: Boolean, default: false },
                activeTab:        { type: String,  default: 'pending' },
                operating:        { type: Boolean, default: false },
            },

            emits: ['toggle-select', 'approve', 'reject', 'queue', 'rematch', 'rescore'],

            setup(props, { emit }) {
                const menuOpen = ref(false);

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

                const closeMenu = () => { menuOpen.value = false; };

                onMounted(() => { document.addEventListener('click', closeMenu); });
                onUnmounted(() => { document.removeEventListener('click', closeMenu); });

                return { menuOpen, formatSize };
            },

            template: `
                <div
                    :class="[
                        'bg-card border rounded-lg shadow-md transition-all duration-200 overflow-hidden cursor-pointer relative',
                        selected
                            ? 'border-accent bg-accent-surface shadow-lg shadow-curator-500/10'
                            : 'border-subtle hover:border-curator-500 hover:shadow-lg'
                    ]"
                    @click="$emit('toggle-select')"
                >
                    <!-- Card Header -->
                    <div class="flex items-center justify-between p-4 border-b border-subtle bg-raised/50">
                        <div class="flex items-center gap-3">
                            <!-- Selection indicator replaces checkbox -->
                            <span v-if="selected" class="w-5 h-5 rounded bg-accent flex items-center justify-center text-gray-900 font-bold text-xs shrink-0">&#10003;</span>
                            <span v-else class="w-5 h-5 rounded border border-gray-600 shrink-0"></span>
                            <span :class="[
                                'inline-block px-2 py-1 rounded text-xs font-mono font-bold uppercase',
                                torrent.status === 'pending'  ? 'badge-blue border' :
                                torrent.status === 'accepted' ? 'badge-accent border' :
                                'badge-red border'
                            ]">
                                {{ torrent.status }}
                            </span>
                        </div>
                        <!-- Kebab options menu: hidden during multi-select -->
                        <div class="relative" @click.stop v-show="!multiSelectActive">
                            <button
                                @click="menuOpen = !menuOpen"
                                class="p-1.5 rounded fg-dim hover:text-curator-500 hover:bg-gray-700 transition-colors duration-150 text-lg leading-none font-bold"
                                title="More options"
                            >&#8942;</button>
                            <div v-if="menuOpen" class="absolute right-0 top-8 z-50 bg-raised border border-base rounded-lg shadow-xl py-1" style="min-width:168px">
                                <button
                                    @click="$emit('rematch')"
                                    class="w-full text-left px-4 py-2 text-sm font-mono fg-soft hover:bg-gray-700 hover:text-curator-500 transition-colors flex items-center gap-2"
                                >&#10227; re-match</button>
                                <button
                                    @click="$emit('rescore')"
                                    class="w-full text-left px-4 py-2 text-sm font-mono fg-soft hover:bg-gray-700 hover:text-curator-500 transition-colors flex items-center gap-2"
                                >&#9889; re-score</button>
                                <button
                                    v-if="torrent.status === 'accepted'"
                                    @click="$emit('queue')"
                                    class="w-full text-left px-4 py-2 text-sm font-mono fg-soft hover:bg-gray-700 hover:text-curator-500 transition-colors flex items-center gap-2"
                                >&#8595; queue for dl</button>
                            </div>
                        </div>
                    </div>

                    <!-- Card Body -->
                    <div class="p-6">
                        <h3 class="text-lg font-bold fg-base mb-4 line-clamp-2 break-words font-mono">{{ torrent.title }}</h3>

                        <div class="space-y-3 mb-6 text-sm">
                            <div class="flex items-center justify-between">
                                <span class="fg-dim font-mono">size:</span>
                                <span class="fg-accent font-mono font-bold">{{ formatSize(torrent.size) }}</span>
                            </div>
                            <div class="flex items-center justify-between">
                                <span class="fg-dim font-mono">match:</span>
                                <span class="fg-accent font-mono font-bold px-2 py-1 bg-raised rounded">{{ torrent.match_reason }}</span>
                            </div>
                            <div v-if="torrent.ai_scored" class="flex items-center justify-between">
                                <span class="fg-dim font-mono">ai score:</span>
                                <span class="font-mono font-bold px-2 py-1 rounded text-xs badge-emerald border" :title="torrent.ai_reason">&#9889; {{ Math.round(torrent.ai_score * 100) }}%</span>
                            </div>
                            <div v-if="torrent.ai_scored && torrent.match_confidence >= 0 && torrent.match_confidence < 0.5" class="flex items-center justify-between">
                                <span class="fg-dim font-mono">match:</span>
                                <span class="font-mono font-bold px-2 py-1 rounded text-xs badge-amber border" :title="torrent.match_confidence_reason">&#9888; low confidence</span>
                            </div>
                        </div>

                        <!-- Card Actions: visible when card is selected -->
                        <div v-if="activeTab === 'pending'" v-show="selected && !multiSelectActive" class="flex gap-3">
                            <button
                                @click.stop="$emit('approve')"
                                :disabled="operating"
                                class="flex-1 px-4 py-2 bg-accent hover:bg-accent disabled:opacity-50 disabled:cursor-not-allowed text-gray-900 rounded font-mono font-bold text-sm transition-colors duration-200 flex items-center justify-center gap-2 uppercase"
                            >
                                <span v-if="operating" class="inline-block w-4 h-4 border-2 border-gray-900 border-t-transparent rounded-full animate-spin"></span>
                                <span v-else>&#10003;</span>
                                {{ operating ? '' : 'accept' }}
                            </button>
                            <button
                                @click.stop="$emit('reject')"
                                :disabled="operating"
                                class="flex-1 px-4 py-2 bg-red-600 hover:bg-red-700 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded font-mono font-bold text-sm transition-colors duration-200 flex items-center justify-center gap-2 uppercase"
                            >
                                <span v-if="operating" class="inline-block w-4 h-4 border-2 border-white border-t-transparent rounded-full animate-spin"></span>
                                <span v-else>&#10005;</span>
                                {{ operating ? '' : 'reject' }}
                            </button>
                        </div>

                        <div v-if="activeTab === 'accepted'" v-show="selected && !multiSelectActive">
                            <button
                                @click.stop="$emit('queue')"
                                :disabled="operating"
                                class="w-full px-4 py-2 bg-accent hover:bg-accent disabled:opacity-50 disabled:cursor-not-allowed text-gray-900 rounded font-mono font-bold text-sm transition-colors duration-200 flex items-center justify-center gap-2 uppercase"
                                title="Queue this torrent for download"
                            >
                                <span v-if="operating" class="inline-block w-4 h-4 border-2 border-gray-900 border-t-transparent rounded-full animate-spin"></span>
                                <span v-else>&#8595;</span>
                                {{ operating ? 'queuing...' : 'queue for dl' }}
                            </button>
                        </div>
                    </div>
                </div>
            `,
        });
    }

    global.registerTorrentCardComponent = registerTorrentCardComponent;
}(window));
