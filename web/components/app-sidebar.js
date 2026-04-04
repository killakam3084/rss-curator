(function (global) {
    function registerAppSidebarComponent(app) {
        app.component('app-sidebar', {
            props: {
                collapsed:    { type: Boolean,  required: true },
                stats:        { type: Object,   required: true },
                tab:          { type: String,   default: 'activity' },
                activities:   { type: Array,    default: () => [] },
                feedStream:   { type: Array,    default: () => [] },
                formatSizeFn: { type: Function, required: true },
            },

            emits: ['toggle-collapse', 'update:tab'],

            data() {
                return {
                    cpuHistory:    [],
                    memHistory:    [],
                    netInHistory:  [],
                    netOutHistory: [],
                    metricsTimer:  null,
                };
            },

            computed: {
                cpuCurrent() {
                    if (!this.cpuHistory.length) return '—';
                    return this.cpuHistory[this.cpuHistory.length - 1].toFixed(1) + '%';
                },
                memCurrent() {
                    if (!this.memHistory.length) return '—';
                    return this.memHistory[this.memHistory.length - 1].toFixed(1) + '%';
                },
                netInCurrent() {
                    if (!this.netInHistory.length) return '—';
                    return this.formatBps(this.netInHistory[this.netInHistory.length - 1]);
                },
                netOutCurrent() {
                    if (!this.netOutHistory.length) return '—';
                    return this.formatBps(this.netOutHistory[this.netOutHistory.length - 1]);
                },
            },

            methods: {
                fetchMetrics() {
                    fetch('/api/metrics')
                        .then(r => r.ok ? r.json() : null)
                        .then(data => {
                            if (!data) return;
                            this.cpuHistory.push(data.cpu_pct);
                            if (this.cpuHistory.length > 30) this.cpuHistory.shift();
                            this.memHistory.push(data.mem_pct);
                            if (this.memHistory.length > 30) this.memHistory.shift();
                            this.netInHistory.push(data.net_in_bps);
                            if (this.netInHistory.length > 30) this.netInHistory.shift();
                            this.netOutHistory.push(data.net_out_bps);
                            if (this.netOutHistory.length > 30) this.netOutHistory.shift();
                        })
                        .catch(() => {});
                },
                formatBps(bps) {
                    if (bps < 1024)           return bps.toFixed(0) + 'B/s';
                    if (bps < 1024 * 1024)    return (bps / 1024).toFixed(0) + 'K/s';
                    return (bps / (1024 * 1024)).toFixed(1) + 'M/s';
                },
                // Returns space-separated "x,y" points for a <polyline> inside viewBox "0 0 200 20".
                sparklinePoints(history, maxVal) {
                    const n = history.length;
                    if (n < 2) return '';
                    const w = 200, h = 20, pad = 2;
                    const max = (maxVal != null && maxVal > 0)
                        ? maxVal
                        : (Math.max(...history) || 1);
                    const step = w / (n - 1);
                    return history.map((v, i) => {
                        const x = (i * step).toFixed(1);
                        const y = ((h - pad) - (Math.min(v, max) / max) * (h - 2 * pad)).toFixed(1);
                        return `${x},${y}`;
                    }).join(' ');
                },
                // Returns the same points closed into a polygon for the filled area.
                sparklineArea(history, maxVal) {
                    const n = history.length;
                    if (n < 2) return '';
                    const w = 200, h = 20, pad = 2;
                    const max = (maxVal != null && maxVal > 0)
                        ? maxVal
                        : (Math.max(...history) || 1);
                    const step = w / (n - 1);
                    const pts = history.map((v, i) => {
                        const x = (i * step).toFixed(1);
                        const y = ((h - pad) - (Math.min(v, max) / max) * (h - 2 * pad)).toFixed(1);
                        return `${x},${y}`;
                    }).join(' ');
                    const lastX = ((n - 1) * step).toFixed(1);
                    return `${pts} ${lastX},${h} 0,${h}`;
                },
            },

            mounted() {
                this.fetchMetrics();
                this.metricsTimer = setInterval(this.fetchMetrics, 750);
            },

            beforeUnmount() {
                clearInterval(this.metricsTimer);
            },

            template: `
                <aside :style="{width: collapsed ? '64px' : '320px'}" class="fixed top-14 left-0 h-[calc(100vh-3.5rem)] bg-card border-r border-subtle shadow-2xl transition-all duration-300 z-30 flex flex-col">
                    <!-- Console Header with Hamburger Toggle -->
                    <div class="flex items-center justify-between p-4 border-b border-subtle bg-raised/50 shrink-0">
                        <h2 v-if="!collapsed" class="text-sm font-bold font-mono fg-accent uppercase">
                            > console
                        </h2>
                        <button
                            @click="$emit('toggle-collapse')"
                            class="p-2 rounded border border-base bg-raised hover:border-curator-500 transition-colors duration-200"
                            :title="collapsed ? 'Expand console' : 'Collapse console'"
                        >
                            <span class="fg-accent font-bold text-lg">{{ collapsed ? '☰' : '✕' }}</span>
                        </button>
                    </div>

                    <!-- Stats Mini-Panel -->
                    <div class="border-b border-subtle shrink-0">
                        <!-- Collapsed: pending badge only -->
                        <div v-if="collapsed" class="p-3 flex flex-col items-center gap-1">
                            <span class="text-xs font-mono fg-muted uppercase">q</span>
                            <span class="text-sm font-bold font-mono fg-accent">{{ stats.pending }}</span>
                        </div>
                        <!-- Expanded: 6-tile grid -->
                        <div v-else class="p-3">
                            <div class="flex items-center gap-1.5 mb-2 pb-1.5 border-b border-subtle">
                                <svg class="w-3 h-3 fg-dim shrink-0" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                                    <rect x="3" y="14" width="4" height="7" rx="0.5"/>
                                    <rect x="10" y="9" width="4" height="12" rx="0.5"/>
                                    <rect x="17" y="4" width="4" height="17" rx="0.5"/>
                                </svg>
                                <span class="text-xs font-mono fg-dim font-bold uppercase tracking-widest">stats</span>
                            </div>
                            <div class="grid grid-cols-2 gap-2">
                            <div class="bg-raised rounded p-2 text-center">
                                <p class="text-xs font-mono fg-dim uppercase leading-none mb-1">pending</p>
                                <p class="text-lg font-bold font-mono fg-accent">{{ stats.pending }}</p>
                            </div>
                            <div class="bg-raised rounded p-2 text-center">
                                <p class="text-xs font-mono fg-dim uppercase leading-none mb-1">seen 24h</p>
                                <p class="text-lg font-bold font-mono fg-accent">{{ stats.seen }}</p>
                            </div>
                            <div class="bg-raised rounded p-2 text-center">
                                <p class="text-xs font-mono fg-dim uppercase leading-none mb-1">staged 24h</p>
                                <p class="text-lg font-mono font-bold fg-accent">{{ stats.staged }}</p>
                            </div>
                            <div class="bg-raised rounded p-2 text-center">
                                <p class="text-xs font-mono fg-dim uppercase leading-none mb-1">approved 24h</p>
                                <p class="text-lg font-bold font-mono fg-accent">{{ stats.approved }}</p>
                            </div>
                            <div class="bg-raised rounded p-2 text-center">
                                <p class="text-xs font-mono fg-dim uppercase leading-none mb-1">rejected 24h</p>
                                <p class="text-lg font-bold font-mono fg-accent">{{ stats.rejected }}</p>
                            </div>
                            <div class="bg-raised rounded p-2 text-center">
                                <p class="text-xs font-mono fg-dim uppercase leading-none mb-1">queued 24h</p>
                                <p class="text-lg font-bold font-mono fg-accent">{{ stats.queued }}</p>
                            </div>
                            </div>
                        </div>
                    </div>

                    <!-- System Metrics -->
                    <div v-if="!collapsed" class="border-b border-subtle px-3 py-2.5 shrink-0">
                        <div class="flex items-center gap-1.5 mb-2 pb-1.5 border-b border-subtle">
                            <svg class="w-3 h-3 fg-dim shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                                <polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/>
                            </svg>
                            <span class="text-xs font-mono fg-dim font-bold uppercase tracking-widest">system</span>
                        </div>
                        <!-- CPU -->
                        <div class="flex items-center gap-2 mb-1.5">
                            <span class="text-xs font-mono fg-dim w-7 shrink-0 leading-none">cpu</span>
                            <div class="flex-1 min-w-0">
                                <svg viewBox="0 0 200 20" preserveAspectRatio="none" width="100%" height="20" class="fg-accent block">
                                    <polygon v-if="cpuHistory.length > 1" :points="sparklineArea(cpuHistory, 100)" fill="currentColor" opacity="0.12"/>
                                    <polyline v-if="cpuHistory.length > 1" :points="sparklinePoints(cpuHistory, 100)" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" stroke-linecap="round"/>
                                </svg>
                            </div>
                            <span class="text-xs font-mono fg-accent w-[46px] text-right shrink-0 leading-none tabular-nums">{{ cpuCurrent }}</span>
                        </div>
                        <!-- Mem -->
                        <div class="flex items-center gap-2 mb-1.5">
                            <span class="text-xs font-mono fg-dim w-7 shrink-0 leading-none">mem</span>
                            <div class="flex-1 min-w-0">
                                <svg viewBox="0 0 200 20" preserveAspectRatio="none" width="100%" height="20" class="text-violet-400 block">
                                    <polygon v-if="memHistory.length > 1" :points="sparklineArea(memHistory, 100)" fill="currentColor" opacity="0.12"/>
                                    <polyline v-if="memHistory.length > 1" :points="sparklinePoints(memHistory, 100)" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" stroke-linecap="round"/>
                                </svg>
                            </div>
                            <span class="text-xs font-mono text-violet-400 w-[46px] text-right shrink-0 leading-none tabular-nums">{{ memCurrent }}</span>
                        </div>
                        <!-- Net In -->
                        <div class="flex items-center gap-2 mb-1.5">
                            <span class="text-xs font-mono fg-dim w-7 shrink-0 leading-none">▲ in</span>
                            <div class="flex-1 min-w-0">
                                <svg viewBox="0 0 200 20" preserveAspectRatio="none" width="100%" height="20" class="text-emerald-400 block">
                                    <polygon v-if="netInHistory.length > 1" :points="sparklineArea(netInHistory)" fill="currentColor" opacity="0.12"/>
                                    <polyline v-if="netInHistory.length > 1" :points="sparklinePoints(netInHistory)" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" stroke-linecap="round"/>
                                </svg>
                            </div>
                            <span class="text-xs font-mono text-emerald-400 w-[46px] text-right shrink-0 leading-none tabular-nums">{{ netInCurrent }}</span>
                        </div>
                        <!-- Net Out -->
                        <div class="flex items-center gap-2">
                            <span class="text-xs font-mono fg-dim w-7 shrink-0 leading-none">▼ out</span>
                            <div class="flex-1 min-w-0">
                                <svg viewBox="0 0 200 20" preserveAspectRatio="none" width="100%" height="20" class="text-sky-400 block">
                                    <polygon v-if="netOutHistory.length > 1" :points="sparklineArea(netOutHistory)" fill="currentColor" opacity="0.12"/>
                                    <polyline v-if="netOutHistory.length > 1" :points="sparklinePoints(netOutHistory)" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" stroke-linecap="round"/>
                                </svg>
                            </div>
                            <span class="text-xs font-mono text-sky-400 w-[46px] text-right shrink-0 leading-none tabular-nums">{{ netOutCurrent }}</span>
                        </div>
                    </div>

                    <!-- Tab Switcher -->
                    <div v-if="!collapsed" class="flex gap-1 p-3 border-b border-subtle bg-raised/30 shrink-0">
                        <button
                            @click="$emit('update:tab', 'activity')"
                            :class="[
                                'flex-1 px-3 py-2 rounded text-xs font-mono font-bold uppercase transition-all duration-200',
                                tab === 'activity'
                                    ? 'bg-accent text-gray-900'
                                    : 'bg-raised fg-soft hover:text-curator-500 border border-base'
                            ]"
                        >
                            activity
                        </button>
                        <button
                            @click="$emit('update:tab', 'feed')"
                            :class="[
                                'flex-1 px-3 py-2 rounded text-xs font-mono font-bold uppercase transition-all duration-200',
                                tab === 'feed'
                                    ? 'bg-accent text-gray-900'
                                    : 'bg-raised fg-soft hover:text-curator-500 border border-base'
                            ]"
                        >
                            feed stream
                        </button>
                    </div>

                    <!-- Content -->
                    <div v-if="!collapsed" class="flex-1 min-h-0 p-4 overflow-y-auto">
                        <!-- Activity Log Tab -->
                        <div v-if="tab === 'activity'">
                            <div v-if="activities.length === 0" class="text-center py-8">
                                <p class="fg-dim text-sm font-mono">no activity</p>
                            </div>

                            <div v-else class="space-y-3">
                                <div
                                    v-for="activity in activities"
                                    :key="activity.id"
                                    class="p-3 rounded border-l-4 bg-raised/50 transition-all duration-200 border-accent animate-in fade-in slide-in-from-top-2"
                                >
                                    <div class="flex items-center gap-2 mb-2">
                                        <span class="inline-block px-2 py-1 rounded text-xs font-mono font-bold uppercase fg-accent">
                                            {{ activity.action }}
                                        </span>
                                    </div>
                                    <p class="text-sm font-mono fg-base line-clamp-2">{{ activity.torrent_title }}</p>
                                    <p class="text-xs fg-dim font-mono mt-1">{{ activity.match_reason }}</p>
                                    <p class="text-xs fg-muted font-mono mt-2">{{ activity.action_at }}</p>
                                </div>
                            </div>
                        </div>

                        <!-- Feed Stream Tab -->
                        <div v-if="tab === 'feed'">
                            <div v-if="feedStream.length === 0" class="text-center py-8">
                                <p class="fg-dim text-sm font-mono">no feed data</p>
                            </div>

                            <div v-else class="space-y-2">
                                <div
                                    v-for="item in feedStream"
                                    :key="item.id"
                                    :class="[
                                        'p-3 rounded border-l-4 text-xs font-mono transition-all duration-300 animate-in fade-in slide-in-from-top-2',
                                        item.status === 'pending'
                                            ? 'bg-raised/30 border-subtle fg-soft'
                                            : 'bg-raised/60 border-accent fg-soft'
                                    ]"
                                >
                                    <div class="flex items-start justify-between gap-2 mb-2">
                                        <span class="font-bold fg-base line-clamp-2 text-xs">{{ item.title }}</span>
                                        <span v-if="item.status !== 'pending'" class="fg-accent font-bold shrink-0">✓</span>
                                    </div>
                                    <div class="flex items-center justify-between fg-dim text-xs mb-1">
                                        <span>{{ formatSizeFn(item.size) }}</span>
                                    </div>
                                    <div class="fg-muted text-xs">
                                        <span class="fg-accent">{{ item.match_reason }}</span>
                                    </div>
                                </div>
                            </div>
                        </div>
                    </div>
                </aside>
            `,
        });
    }

    global.registerAppSidebarComponent = registerAppSidebarComponent;
}(window));
