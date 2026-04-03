/**
 * log-viewer component
 *
 * Two variants controlled by the `variant` prop:
 *
 *   variant="drawer"  — fixed bottom-anchored DevTools-style drawer.
 *                       Controlled externally via :open / @close / @height-change.
 *                       Used on index.html.
 *
 *   variant="panel"   — inline collapsible section.
 *                       Manages its own open/closed state.
 *                       Used on jobs.html and settings.html.
 *
 * The component owns all log state: SSE connection, entries, filters,
 * auto-scroll, drag-to-resize (drawer only).
 */
(function (global) {
    function registerLogViewerComponent(app) {
        const { ref, computed, watch, onUnmounted, nextTick } = Vue;

        app.component('log-viewer', {
            props: {
                // 'drawer' | 'panel'
                variant:      { type: String,  default: 'panel' },
                // drawer variant: externally controlled open state
                open:         { type: Boolean, default: false },
                // drawer variant: CSS height string (e.g. '60vh', '300px')
                drawerHeight: { type: String,  default: '60vh' },
                // drawer variant: CSS right offset (e.g. '320px', '64px')
                drawerRight:  { type: String,  default: '320px' },
                // label shown in the toolbar
                title:        { type: String,  default: '// application logs' },
            },

            emits: ['close', 'height-change'],

            setup(props, { emit }) {
                // ── Log state ──────────────────────────────────────────────
                const logEntries     = ref([]);
                const logFilter      = ref('');
                const logLevelFilter = ref(['INFO', 'WARN', 'ERROR', 'DEBUG', 'FATAL']);
                const logAutoScroll  = ref(true);
                const logSortDesc    = ref(true); // true = newest first
                const resizing       = ref(false); // suppresses CSS transition during drag

                // panel variant: internal open/close state
                const panelOpen = ref(false);

                let logEventSource = null;

                // ── Computed ───────────────────────────────────────────────
                const filteredLogs = computed(() => {
                    const filtered = logEntries.value.filter(e => {
                        const levelMatch = logLevelFilter.value.includes(e.level);
                        const textMatch  = !logFilter.value ||
                            e.message.toLowerCase().includes(logFilter.value.toLowerCase());
                        return levelMatch && textMatch;
                    });
                    return logSortDesc.value
                        ? filtered.slice().sort((a, b) => b.id - a.id)
                        : filtered.slice().sort((a, b) => a.id - b.id);
                });

                // True when the log stream is connected and active.
                const streamActive = computed(() =>
                    props.variant === 'drawer' ? props.open : panelOpen.value
                );

                // ── SSE helpers ────────────────────────────────────────────
                const openStream = async () => {
                    // Backfill with buffered history first.
                    try {
                        const res  = await fetch('/api/logs');
                        const data = await res.json();
                        if (Array.isArray(data)) logEntries.value = data;
                    } catch (_) {}

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
                                    const el = document.getElementById('lv-log-body');
                                    if (el) el.scrollTop = logSortDesc.value ? 0 : el.scrollHeight;
                                });
                            }
                        } catch (_) {}
                    };
                    logEventSource.onerror = () => {};
                };

                const closeStream = () => {
                    if (logEventSource) {
                        logEventSource.close();
                        logEventSource = null;
                    }
                };

                // ── SSE lifecycle watchers ─────────────────────────────────
                // Drawer variant: host controls open prop.
                watch(() => props.open, (isOpen) => {
                    if (props.variant !== 'drawer') return;
                    if (isOpen) openStream();
                    else        closeStream();
                }, { immediate: true });

                // Panel variant: component controls panelOpen.
                watch(panelOpen, (isOpen) => {
                    if (props.variant !== 'panel') return;
                    if (isOpen) openStream();
                    else        closeStream();
                });

                onUnmounted(closeStream);

                // ── Actions ────────────────────────────────────────────────
                const clearLogs = () => { logEntries.value = []; };

                const toggleLevelFilter = (level) => {
                    const idx = logLevelFilter.value.indexOf(level);
                    if (idx === -1) {
                        logLevelFilter.value.push(level);
                    } else if (logLevelFilter.value.length > 1) {
                        logLevelFilter.value.splice(idx, 1);
                    }
                };

                // Drag-to-resize — only meaningful for drawer variant.
                const startResize = (e) => {
                    e.preventDefault();
                    // Use the outer wrapper element's height as start reference.
                    const startY      = e.clientY;
                    const startHeight = parseInt(props.drawerHeight, 10) ||
                        (window.innerHeight * 0.6);
                    resizing.value = true;
                    const onMove = (mv) => {
                        const delta   = startY - mv.clientY; // drag up → taller
                        const clamped = Math.max(80, Math.min(window.innerHeight * 0.92, startHeight + delta));
                        emit('height-change', clamped + 'px');
                    };
                    const onUp = () => {
                        resizing.value = false;
                        document.removeEventListener('mousemove', onMove);
                        document.removeEventListener('mouseup', onUp);
                    };
                    document.addEventListener('mousemove', onMove);
                    document.addEventListener('mouseup', onUp);
                };

                return {
                    logEntries, logFilter, logLevelFilter, logAutoScroll, logSortDesc,
                    resizing, panelOpen, streamActive, filteredLogs,
                    clearLogs, toggleLevelFilter, startResize,
                };
            },

            template: `
                <!-- ═══════════════════════════════════════════════════
                     DRAWER variant (fixed bottom, floating DevTools panel)
                     ═══════════════════════════════════════════════════ -->
                <template v-if="variant === 'drawer'">
                    <div
                        :style="{
                            height:     open ? drawerHeight : '0',
                            right:      drawerRight,
                            transition: resizing ? 'none' : 'height 0.25s cubic-bezier(0.4,0,0.2,1)'
                        }"
                        class="fixed bottom-0 left-0 z-50 overflow-hidden bg-gray-950 border-t-2 border-curator-500/40 shadow-2xl flex flex-col"
                    >
                        <div class="flex items-center gap-2 px-4 py-2 border-b border-gray-800 bg-gray-900 shrink-0 flex-wrap">
                            <div
                                class="w-8 h-1 rounded-full bg-gray-700 mr-1 cursor-ns-resize select-none"
                                @mousedown="startResize"
                            ></div>
                            <span class="text-xs font-mono text-curator-500 font-bold whitespace-nowrap">{{ title }}</span>
                            <span
                                :class="['w-2 h-2 rounded-full shrink-0', open ? 'bg-curator-500 animate-pulse' : 'bg-gray-600']"
                                title="SSE stream"
                            ></span>
                            <div class="flex-1"></div>
                            <button
                                v-for="level in ['INFO', 'WARN', 'ERROR']"
                                :key="level"
                                @click="toggleLevelFilter(level)"
                                :class="[
                                    'px-2 py-0.5 rounded text-xs font-mono font-bold uppercase transition-colors',
                                    logLevelFilter.includes(level)
                                        ? level === 'WARN'  ? 'bg-yellow-900/60 text-yellow-400 border border-yellow-700/50'
                                        : level === 'ERROR' ? 'bg-red-900/60 text-red-400 border border-red-700/50'
                                        : 'bg-gray-700 text-gray-300 border border-gray-600'
                                        : 'bg-gray-900 text-gray-600 border border-gray-800'
                                ]"
                            >{{ level }}</button>
                            <input
                                v-model="logFilter"
                                type="text"
                                placeholder="filter..."
                                class="px-2 py-0.5 bg-gray-800 border border-gray-700 rounded text-xs font-mono text-gray-300 placeholder-gray-600 focus:border-curator-500 focus:outline-none w-28"
                            >
                            <button
                                @click="logAutoScroll = !logAutoScroll"
                                :class="['px-2 py-0.5 rounded text-xs font-mono uppercase border transition-colors', logAutoScroll ? 'bg-curator-500/20 text-curator-500 border-curator-500/50' : 'bg-gray-800 text-gray-500 border-gray-700']"
                                title="Toggle auto-scroll"
                            >&#8595;</button>
                            <button
                                @click="logSortDesc = !logSortDesc"
                                :title="logSortDesc ? 'Newest first — click for oldest first' : 'Oldest first — click for newest first'"
                                class="px-2 py-0.5 rounded text-xs font-mono uppercase bg-gray-800 text-gray-500 border border-gray-700 hover:border-curator-500 hover:text-curator-500 transition-colors"
                            >{{ logSortDesc ? '↓ new' : '↑ old' }}</button>
                            <button
                                @click="clearLogs"
                                class="px-2 py-0.5 rounded text-xs font-mono uppercase bg-gray-800 text-gray-500 border border-gray-700 hover:border-red-700 hover:text-red-400 transition-colors"
                            >clear</button>
                            <button
                                @click="$emit('close')"
                                class="px-2 py-0.5 rounded text-xs font-mono uppercase bg-gray-800 text-gray-500 border border-gray-700 hover:border-curator-500 hover:text-curator-500 transition-colors"
                            >&#10005;</button>
                        </div>
                        <div id="lv-log-body" class="flex-1 overflow-y-auto font-mono text-xs p-2 bg-gray-950">
                            <div v-if="filteredLogs.length === 0" class="text-gray-600 p-4 text-center">no log entries</div>
                            <div
                                v-for="entry in filteredLogs"
                                :key="entry.id"
                                class="flex items-baseline gap-2 px-2 py-0.5 rounded hover:bg-gray-900/60"
                            >
                                <span :class="[
                                    'shrink-0 px-1 rounded text-xs font-bold uppercase w-12 text-center',
                                    entry.level === 'ERROR' || entry.level === 'FATAL' ? 'bg-red-900/50 text-red-400' :
                                    entry.level === 'WARN'  ? 'bg-yellow-900/50 text-yellow-400' :
                                    entry.level === 'DEBUG' ? 'bg-blue-900/50 text-blue-400' :
                                    'bg-gray-800 text-gray-500'
                                ]">{{ entry.level }}</span>
                                <span class="shrink-0 text-gray-600">{{ new Date(entry.time).toLocaleTimeString() }}</span>
                                <span class="text-gray-300 break-all">{{ entry.message }}</span>
                                <span v-if="entry.fields && Object.keys(entry.fields).length" class="text-gray-600 break-all">
                                    {{ Object.entries(entry.fields).map(([k,v]) => k + '=' + v).join(' ') }}
                                </span>
                            </div>
                        </div>
                    </div>
                </template>

                <!-- ═══════════════════════════════════════════════════
                     PANEL variant (inline collapsible section)
                     ═══════════════════════════════════════════════════ -->
                <template v-else>
                    <section class="bg-gray-900 border border-gray-800 rounded-lg overflow-hidden">
                        <!-- Header / toggle bar -->
                        <button
                            @click="panelOpen = !panelOpen"
                            class="w-full flex items-center gap-3 px-4 py-3 hover:bg-gray-800/50 transition-colors duration-150 group"
                        >
                            <svg
                                :class="['w-3 h-3 text-gray-500 transition-transform duration-200 shrink-0', panelOpen ? 'rotate-90' : '']"
                                fill="none" stroke="currentColor" stroke-width="2.5" viewBox="0 0 24 24"
                            ><path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7"/></svg>
                            <span class="text-xs font-mono font-bold text-curator-500">{{ title }}</span>
                            <span
                                v-if="panelOpen"
                                class="w-2 h-2 rounded-full bg-curator-500 animate-pulse shrink-0"
                                title="SSE stream active"
                            ></span>
                            <span class="ml-auto text-xs font-mono text-gray-600 group-hover:text-gray-400 transition-colors">
                                {{ panelOpen ? 'collapse' : 'expand' }}
                            </span>
                        </button>

                        <!-- Collapsible body -->
                        <div v-if="panelOpen" class="border-t border-gray-800">
                            <!-- Toolbar -->
                            <div class="flex items-center gap-2 px-4 py-2 bg-gray-900 flex-wrap">
                                <button
                                    v-for="level in ['INFO', 'WARN', 'ERROR']"
                                    :key="level"
                                    @click="toggleLevelFilter(level)"
                                    :class="[
                                        'px-2 py-0.5 rounded text-xs font-mono font-bold uppercase transition-colors',
                                        logLevelFilter.includes(level)
                                            ? level === 'WARN'  ? 'bg-yellow-900/60 text-yellow-400 border border-yellow-700/50'
                                            : level === 'ERROR' ? 'bg-red-900/60 text-red-400 border border-red-700/50'
                                            : 'bg-gray-700 text-gray-300 border border-gray-600'
                                            : 'bg-gray-900 text-gray-600 border border-gray-800'
                                    ]"
                                >{{ level }}</button>
                                <input
                                    v-model="logFilter"
                                    type="text"
                                    placeholder="filter..."
                                    class="px-2 py-0.5 bg-gray-800 border border-gray-700 rounded text-xs font-mono text-gray-300 placeholder-gray-600 focus:border-curator-500 focus:outline-none w-28"
                                >
                                <div class="flex-1"></div>
                                <button
                                    @click="logAutoScroll = !logAutoScroll"
                                    :class="['px-2 py-0.5 rounded text-xs font-mono uppercase border transition-colors', logAutoScroll ? 'bg-curator-500/20 text-curator-500 border-curator-500/50' : 'bg-gray-800 text-gray-500 border-gray-700']"
                                    title="Toggle auto-scroll"
                                >&#8595;</button>
                                <button
                                    @click="logSortDesc = !logSortDesc"
                                    :title="logSortDesc ? 'Newest first — click for oldest first' : 'Oldest first — click for newest first'"
                                    class="px-2 py-0.5 rounded text-xs font-mono uppercase bg-gray-800 text-gray-500 border border-gray-700 hover:border-curator-500 hover:text-curator-500 transition-colors"
                                >{{ logSortDesc ? '↓ new' : '↑ old' }}</button>
                                <button
                                    @click="clearLogs"
                                    class="px-2 py-0.5 rounded text-xs font-mono uppercase bg-gray-800 text-gray-500 border border-gray-700 hover:border-red-700 hover:text-red-400 transition-colors"
                                >clear</button>
                            </div>
                            <!-- Log body -->
                            <div id="lv-log-body" class="h-64 overflow-y-auto font-mono text-xs p-2 bg-gray-950">
                                <div v-if="filteredLogs.length === 0" class="text-gray-600 p-4 text-center">no log entries</div>
                                <div
                                    v-for="entry in filteredLogs"
                                    :key="entry.id"
                                    class="flex items-baseline gap-2 px-2 py-0.5 rounded hover:bg-gray-900/60"
                                >
                                    <span :class="[
                                        'shrink-0 px-1 rounded text-xs font-bold uppercase w-12 text-center',
                                        entry.level === 'ERROR' || entry.level === 'FATAL' ? 'bg-red-900/50 text-red-400' :
                                        entry.level === 'WARN'  ? 'bg-yellow-900/50 text-yellow-400' :
                                        entry.level === 'DEBUG' ? 'bg-blue-900/50 text-blue-400' :
                                        'bg-gray-800 text-gray-500'
                                    ]">{{ entry.level }}</span>
                                    <span class="shrink-0 text-gray-600">{{ new Date(entry.time).toLocaleTimeString() }}</span>
                                    <span class="text-gray-300 break-all">{{ entry.message }}</span>
                                    <span v-if="entry.fields && Object.keys(entry.fields).length" class="text-gray-600 break-all">
                                        {{ Object.entries(entry.fields).map(([k,v]) => k + '=' + v).join(' ') }}
                                    </span>
                                </div>
                            </div>
                        </div>
                    </section>
                </template>
            `,
        });
    }

    global.registerLogViewerComponent = registerLogViewerComponent;
})(window);
