(function (global) {
    function registerAppSidebarComponent(app) {
        app.component('app-sidebar', {
            props: {
                collapsed:    { type: Boolean,  required: true },
                darkMode:     { type: Boolean,  required: true },
                stats:        { type: Object,   required: true },
                logsOpen:     { type: Boolean,  required: true },
                tab:          { type: String,   default: 'activity' },
                activities:   { type: Array,    default: () => [] },
                feedStream:   { type: Array,    default: () => [] },
                formatSizeFn: { type: Function, required: true },
            },

            emits: ['toggle-collapse', 'toggle-dark-mode', 'update:tab', 'toggle-logs'],

            template: `
                <aside :style="{width: collapsed ? '64px' : '320px'}" class="fixed top-14 right-0 h-[calc(100vh-3.5rem)] bg-gray-900 border-l border-gray-800 shadow-2xl transition-all duration-300 z-40">
                    <!-- Console Header with Hamburger Toggle -->
                    <div class="flex items-center justify-between p-4 border-b border-gray-800 bg-gray-800/50">
                        <h2 v-if="!collapsed" class="text-sm font-bold font-mono text-curator-500 uppercase">
                            > console
                        </h2>
                        <button
                            @click="$emit('toggle-collapse')"
                            class="p-2 rounded border border-gray-700 bg-gray-800 hover:border-curator-500 transition-colors duration-200"
                            :title="collapsed ? 'Expand console' : 'Collapse console'"
                        >
                            <span class="text-curator-500 font-bold text-lg">{{ collapsed ? '☰' : '✕' }}</span>
                        </button>
                    </div>

                    <!-- Dark Mode Toggle (Always Visible) -->
                    <div class="p-4 border-b border-gray-800">
                        <button
                            @click="$emit('toggle-dark-mode')"
                            :class="['w-full flex items-center gap-3 p-2 rounded border border-gray-700 bg-gray-800 hover:border-curator-500 transition-colors duration-200', collapsed ? 'justify-center' : '']"
                            :title="darkMode ? 'Switch to light mode' : 'Switch to dark mode'"
                        >
                            <span class="text-xl">{{ darkMode ? '☀️' : '🌙' }}</span>
                            <span v-if="!collapsed" class="text-xs font-mono text-gray-400 uppercase">{{ darkMode ? 'light' : 'dark' }}</span>
                        </button>
                    </div>

                    <!-- Stats Mini-Panel -->
                    <div class="border-b border-gray-800">
                        <!-- Collapsed: pending badge only -->
                        <div v-if="collapsed" class="p-3 flex flex-col items-center gap-1">
                            <span class="text-xs font-mono text-gray-600 uppercase">q</span>
                            <span class="text-sm font-bold font-mono text-curator-500">{{ stats.pending }}</span>
                        </div>
                        <!-- Expanded: 6-tile grid -->
                        <div v-else class="p-3">
                            <div class="flex items-center gap-1.5 mb-2 pb-1.5 border-b border-gray-800/60">
                                <svg class="w-3 h-3 text-gray-500 shrink-0" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                                    <rect x="3" y="14" width="4" height="7" rx="0.5"/>
                                    <rect x="10" y="9" width="4" height="12" rx="0.5"/>
                                    <rect x="17" y="4" width="4" height="17" rx="0.5"/>
                                </svg>
                                <span class="text-xs font-mono text-gray-500 font-bold uppercase tracking-widest">stats</span>
                            </div>
                            <div class="grid grid-cols-2 gap-2">
                            <div class="bg-gray-800 rounded p-2 text-center">
                                <p class="text-xs font-mono text-gray-500 uppercase leading-none mb-1">pending</p>
                                <p class="text-lg font-bold font-mono text-curator-500">{{ stats.pending }}</p>
                            </div>
                            <div class="bg-gray-800 rounded p-2 text-center">
                                <p class="text-xs font-mono text-gray-500 uppercase leading-none mb-1">seen 24h</p>
                                <p class="text-lg font-bold font-mono text-curator-500">{{ stats.seen }}</p>
                            </div>
                            <div class="bg-gray-800 rounded p-2 text-center">
                                <p class="text-xs font-mono text-gray-500 uppercase leading-none mb-1">staged 24h</p>
                                <p class="text-lg font-mono font-bold text-curator-500">{{ stats.staged }}</p>
                            </div>
                            <div class="bg-gray-800 rounded p-2 text-center">
                                <p class="text-xs font-mono text-gray-500 uppercase leading-none mb-1">approved 24h</p>
                                <p class="text-lg font-bold font-mono text-curator-500">{{ stats.approved }}</p>
                            </div>
                            <div class="bg-gray-800 rounded p-2 text-center">
                                <p class="text-xs font-mono text-gray-500 uppercase leading-none mb-1">rejected 24h</p>
                                <p class="text-lg font-bold font-mono text-curator-500">{{ stats.rejected }}</p>
                            </div>
                            <div class="bg-gray-800 rounded p-2 text-center">
                                <p class="text-xs font-mono text-gray-500 uppercase leading-none mb-1">queued 24h</p>
                                <p class="text-lg font-bold font-mono text-curator-500">{{ stats.queued }}</p>
                            </div>
                            </div>
                        </div>
                    </div>

                    <!-- Logs Drawer Trigger -->
                    <div class="border-b border-gray-800">
                        <button
                            @click="$emit('toggle-logs')"
                            :class="['w-full flex items-center gap-3 p-3 transition-colors duration-200', collapsed ? 'justify-center' : '', logsOpen ? 'bg-gray-800/80' : 'hover:bg-gray-800/40']"
                            :title="logsOpen ? 'Close log drawer' : 'Open log drawer'"
                        >
                            <span :class="['font-mono text-base', logsOpen ? 'text-curator-500' : 'text-gray-500']">&#9166;</span>
                            <span v-if="!collapsed" :class="['text-xs font-mono font-bold uppercase', logsOpen ? 'text-curator-500' : 'text-gray-500']">logs</span>
                            <span v-if="!collapsed && logsOpen" class="ml-auto w-2 h-2 rounded-full bg-curator-500 animate-pulse"></span>
                        </button>
                    </div>

                    <!-- Tab Switcher -->
                    <div v-if="!collapsed" class="flex gap-1 p-3 border-b border-gray-800 bg-gray-800/30">
                        <button
                            @click="$emit('update:tab', 'activity')"
                            :class="[
                                'flex-1 px-3 py-2 rounded text-xs font-mono font-bold uppercase transition-all duration-200',
                                tab === 'activity'
                                    ? 'bg-curator-500 text-gray-900'
                                    : 'bg-gray-800 text-gray-400 hover:text-curator-500 border border-gray-700'
                            ]"
                        >
                            activity
                        </button>
                        <button
                            @click="$emit('update:tab', 'feed')"
                            :class="[
                                'flex-1 px-3 py-2 rounded text-xs font-mono font-bold uppercase transition-all duration-200',
                                tab === 'feed'
                                    ? 'bg-curator-500 text-gray-900'
                                    : 'bg-gray-800 text-gray-400 hover:text-curator-500 border border-gray-700'
                            ]"
                        >
                            feed stream
                        </button>
                    </div>

                    <!-- Content -->
                    <div v-if="!collapsed" class="p-4 overflow-y-auto" style="height: calc(100vh - 410px)">
                        <!-- Activity Log Tab -->
                        <div v-if="tab === 'activity'">
                            <div v-if="activities.length === 0" class="text-center py-8">
                                <p class="text-gray-500 text-sm font-mono">no activity</p>
                            </div>

                            <div v-else class="space-y-3">
                                <div
                                    v-for="activity in activities"
                                    :key="activity.id"
                                    class="p-3 rounded border-l-4 bg-gray-800/50 transition-all duration-200 border-curator-500 animate-in fade-in slide-in-from-top-2"
                                >
                                    <div class="flex items-center gap-2 mb-2">
                                        <span class="inline-block px-2 py-1 rounded text-xs font-mono font-bold uppercase text-curator-500">
                                            {{ activity.action }}
                                        </span>
                                    </div>
                                    <p class="text-sm font-mono text-gray-100 line-clamp-2">{{ activity.torrent_title }}</p>
                                    <p class="text-xs text-gray-500 font-mono mt-1">{{ activity.match_reason }}</p>
                                    <p class="text-xs text-gray-600 font-mono mt-2">{{ activity.action_at }}</p>
                                </div>
                            </div>
                        </div>

                        <!-- Feed Stream Tab -->
                        <div v-if="tab === 'feed'">
                            <div v-if="feedStream.length === 0" class="text-center py-8">
                                <p class="text-gray-500 text-sm font-mono">no feed data</p>
                            </div>

                            <div v-else class="space-y-2">
                                <div
                                    v-for="item in feedStream"
                                    :key="item.id"
                                    :class="[
                                        'p-3 rounded border-l-4 text-xs font-mono transition-all duration-300 animate-in fade-in slide-in-from-top-2',
                                        item.status === 'pending'
                                            ? 'bg-gray-800/30 border-gray-700 text-gray-400'
                                            : 'bg-gray-800/60 border-curator-500 text-gray-300'
                                    ]"
                                >
                                    <div class="flex items-start justify-between gap-2 mb-2">
                                        <span class="font-bold text-gray-100 line-clamp-2 text-xs">{{ item.title }}</span>
                                        <span v-if="item.status !== 'pending'" class="text-curator-500 font-bold shrink-0">✓</span>
                                    </div>
                                    <div class="flex items-center justify-between text-gray-500 text-xs mb-1">
                                        <span>{{ formatSizeFn(item.size) }}</span>
                                    </div>
                                    <div class="text-gray-600 text-xs">
                                        <span class="text-curator-500">{{ item.match_reason }}</span>
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
