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
                const infoOpen = ref(false);

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

                const fmtDate = (d) => d ? new Date(d).toLocaleString() : '—';

                const closeMenu = () => { menuOpen.value = false; };

                onMounted(() => { document.addEventListener('click', closeMenu); });
                onUnmounted(() => { document.removeEventListener('click', closeMenu); });

                return { menuOpen, infoOpen, formatSize, fmtDate };
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
                            <!-- Content type badge -->
                            <span
                                :class="[
                                    'inline-flex items-center gap-1 px-2 py-1 rounded text-xs font-mono font-semibold border',
                                    torrent.content_type === 'movie' ? 'badge-purple border' : 'badge-blue border'
                                ]"
                                :title="torrent.content_type === 'movie' ? 'Movie' : 'Show'"
                            >
                                <!-- TV icon for shows -->
                                <svg v-if="torrent.content_type !== 'movie'" xmlns="http://www.w3.org/2000/svg" class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/></svg>
                                <!-- Film icon for movies -->
                                <svg v-if="torrent.content_type === 'movie'" xmlns="http://www.w3.org/2000/svg" class="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="20" rx="2"/><path d="M7 2v20M17 2v20M2 12h20M2 7h5M2 17h5M17 7h5M17 17h5"/></svg>
                                {{ torrent.content_type === 'movie' ? 'movie' : 'show' }}
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
                                    @click="menuOpen = false; infoOpen = true"
                                    class="w-full text-left px-4 py-2 text-sm font-mono fg-soft hover:bg-gray-700 hover:text-curator-500 transition-colors flex items-center gap-2"
                                >&#9432; info</button>
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
                            <div v-if="torrent.content_type === 'movie' && torrent.release_year" class="flex items-center justify-between">
                                <span class="fg-dim font-mono">year:</span>
                                <span class="fg-accent font-mono font-bold">{{ torrent.release_year }}</span>
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
                            <curator-btn :full="true" @click.stop="$emit('approve')" :disabled="operating" :loading="operating">&#10003; accept</curator-btn>
                            <curator-btn :full="true" variant="danger" @click.stop="$emit('reject')" :disabled="operating" :loading="operating">&#10005; reject</curator-btn>
                        </div>

                        <div v-if="activeTab === 'accepted'" v-show="selected && !multiSelectActive">
                            <curator-btn :full="true" @click.stop="$emit('queue')" :disabled="operating" :loading="operating" loading-text="queuing...">&#8595; queue for dl</curator-btn>
                        </div>
                    </div>
                </div>

                <!-- ── Info modal ──────────────────────────────────────────────── -->
                <teleport to="body">
                    <div v-if="infoOpen"
                        class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
                        @click.self="infoOpen = false"
                    >
                        <div class="bg-card border border-base rounded-xl shadow-2xl w-full max-w-lg mx-4 max-h-[85vh] flex flex-col overflow-hidden">
                            <!-- Modal header -->
                            <div class="flex items-start justify-between gap-3 px-5 py-4 border-b border-subtle bg-raised/60">
                                <h3 class="text-sm font-mono fg-accent font-bold leading-snug break-words">{{ torrent.title }}</h3>
                                <button @click="infoOpen = false" class="shrink-0 w-6 h-6 flex items-center justify-center rounded fg-muted hover:fg-base hover:bg-gray-700 transition-colors font-mono text-sm">✕</button>
                            </div>
                            <!-- Modal body -->
                            <div class="overflow-y-auto px-5 py-4 space-y-2 text-xs font-mono">
                                <!-- badges row -->
                                <div class="flex flex-wrap gap-1.5 mb-3">
                                    <span :class="['px-2 py-0.5 rounded border font-bold uppercase', torrent.status === 'pending' ? 'badge-blue border' : torrent.status === 'accepted' ? 'badge-accent border' : 'badge-red border']">{{ torrent.status }}</span>
                                    <span :class="['px-2 py-0.5 rounded border', torrent.content_type === 'movie' ? 'badge-purple border' : 'badge-blue border']">{{ torrent.content_type }}</span>
                                </div>
                                <div class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 items-baseline">
                                    <span class="fg-dim">id</span>
                                    <span class="fg-soft">{{ torrent.id }}</span>

                                    <span class="fg-dim">size</span>
                                    <span class="fg-accent font-bold">{{ formatSize(torrent.size) }}</span>

                                    <span v-if="torrent.release_year" class="fg-dim">year</span>
                                    <span v-if="torrent.release_year" class="fg-soft">{{ torrent.release_year }}</span>

                                    <span class="fg-dim">published</span>
                                    <span class="fg-soft">{{ fmtDate(torrent.pub_date) }}</span>

                                    <span class="fg-dim">staged</span>
                                    <span class="fg-soft">{{ fmtDate(torrent.staged_at) }}</span>

                                    <span class="fg-dim">match</span>
                                    <span class="fg-soft break-words">{{ torrent.match_reason || '—' }}</span>

                                    <template v-if="torrent.ai_scored">
                                        <span class="fg-dim">ai score</span>
                                        <span class="badge-emerald border px-1.5 py-0.5 rounded w-fit font-bold">&#9889; {{ Math.round(torrent.ai_score * 100) }}%</span>

                                        <span class="fg-dim">ai reason</span>
                                        <span class="fg-soft break-words leading-relaxed">{{ torrent.ai_reason || '—' }}</span>
                                    </template>

                                    <template v-if="torrent.ai_scored && torrent.match_confidence !== undefined">
                                        <span class="fg-dim">confidence</span>
                                        <span :class="torrent.match_confidence < 0.5 ? 'badge-amber border px-1.5 py-0.5 rounded w-fit' : 'fg-soft'">{{ (torrent.match_confidence * 100).toFixed(0) }}%</span>

                                        <template v-if="torrent.match_confidence_reason">
                                            <span class="fg-dim">conf. reason</span>
                                            <span class="fg-soft break-words leading-relaxed">{{ torrent.match_confidence_reason }}</span>
                                        </template>
                                    </template>

                                    <template v-if="torrent.link">
                                        <span class="fg-dim">link</span>
                                        <a :href="torrent.link" target="_blank" rel="noopener noreferrer" class="text-indigo-400 hover:underline break-all">{{ torrent.link }}</a>
                                    </template>
                                </div>
                            </div>
                        </div>
                    </div>
                </teleport>
            `,
        });
    }

    global.registerTorrentCardComponent = registerTorrentCardComponent;
}(window));
