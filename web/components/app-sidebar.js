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

            template: `
                <aside :style="{width: collapsed ? '64px' : '320px'}" class="fixed top-14 left-0 h-[calc(100vh-3.5rem)] bg-card border-r border-subtle shadow-2xl transition-all duration-300 z-30">
                    <!-- Console Header with Hamburger Toggle -->
                    <div class="flex items-center justify-between p-4 border-b border-subtle bg-raised/50">
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
                    <div class="border-b border-subtle">
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

                    <!-- Tab Switcher -->
                    <div v-if="!collapsed" class="flex gap-1 p-3 border-b border-subtle bg-raised/30">
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
                    <div v-if="!collapsed" class="p-4 overflow-y-auto" style="height: calc(100vh - 410px)">
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
