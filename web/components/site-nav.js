(function (global) {
    function registerSiteNavComponent(app) {
        app.component('site-nav', {
            props: {
                // 'index' | 'jobs' | 'settings' — drives active-link highlighting
                page:         { type: String,  default: '' },
                // Shows the logs panel toggle button as active when true.
                logsOpen:     { type: Boolean, default: false },
                // Pass the SSE connection state on pages that have a live indicator (jobs page).
                // Leaving it at the default null hides the indicator entirely.
                sseConnected: { default: null },
            },

            emits: ['toggle-logs'],

            setup() {
                const { ref, onMounted } = Vue;

                // 'light' | 'dark' | 'auto'
                const themeMode = ref('auto');

                const systemDark = () => window.matchMedia('(prefers-color-scheme: dark)').matches;

                const applyTheme = () => {
                    const dark = themeMode.value === 'dark' ||
                        (themeMode.value === 'auto' && systemDark());
                    document.documentElement.classList.toggle('dark', dark);
                };

                const cycleTheme = () => {
                    const order = ['light', 'dark', 'auto'];
                    themeMode.value = order[(order.indexOf(themeMode.value) + 1) % 3];
                    applyTheme();
                    if (themeMode.value === 'auto') {
                        localStorage.removeItem('rss-curator-dark-mode');
                    } else {
                        localStorage.setItem('rss-curator-dark-mode', themeMode.value);
                    }
                };

                onMounted(() => {
                    const saved = localStorage.getItem('rss-curator-dark-mode');
                    // Migrate old JSON-boolean format
                    if (saved === 'true')  { themeMode.value = 'dark';  localStorage.setItem('rss-curator-dark-mode', 'dark'); }
                    else if (saved === 'false') { themeMode.value = 'light'; localStorage.setItem('rss-curator-dark-mode', 'light'); }
                    else if (saved === 'dark' || saved === 'light') { themeMode.value = saved; }
                    else { themeMode.value = 'auto'; }
                    applyTheme();

                    // Re-apply when OS appearance changes and we're in auto mode.
                    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
                        if (themeMode.value === 'auto') applyTheme();
                    });
                });

                return { themeMode, cycleTheme };
            },

            template: `
                <nav
                    class="fixed top-0 left-0 right-0 z-40 h-14 bg-card border-b border-subtle flex items-center justify-between px-6"
                >
                    <!-- Left: wordmark + page links -->
                    <div class="flex items-center gap-6">
                        <a href="/" class="text-xl font-bold font-mono fg-accent hover:opacity-80 transition-opacity">&gt; rss-curator</a>
                        <div class="flex items-center gap-1">
                            <a
                                href="/jobs"
                                :class="[
                                    'px-3 py-1 rounded font-mono text-xs uppercase tracking-widest transition-colors duration-150',
                                    page === 'jobs' ? 'fg-accent font-bold' : 'fg-dim hover:fg-soft'
                                ]"
                            >jobs</a>
                            <a
                                href="/settings"
                                :class="[
                                    'px-3 py-1 rounded font-mono text-xs uppercase tracking-widest transition-colors duration-150',
                                    page === 'settings' ? 'fg-accent font-bold' : 'fg-dim hover:fg-soft'
                                ]"
                            >settings</a>
                        </div>
                    </div>

                    <!-- Right: page-specific actions (slot), SSE live indicator (optional), theme toggle, logout -->
                    <div class="flex items-center gap-3">
                        <slot></slot>
                        <span v-if="sseConnected !== null" class="flex items-center gap-2 text-xs font-mono fg-dim">
                            <span :class="['w-2 h-2 rounded-full', sseConnected ? 'bg-accent animate-pulse-dot' : 'bg-gray-600']"></span>
                            {{ sseConnected ? 'live' : 'connecting\u2026' }}
                        </span>
                        <button
                            @click="$emit('toggle-logs')"
                            class="p-2 rounded border transition-colors duration-150"
                            :class="logsOpen
                                ? 'bg-raised border-base fg-accent'
                                : 'border-transparent hover:bg-raised hover:border-base fg-dim'"
                            title="Toggle logs"
                        >
                            <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="1.75" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 10h16M4 14h10M4 18h8"/>
                            </svg>
                        </button>
                        <button
                            @click="cycleTheme"
                            class="p-2 rounded border border-transparent hover:bg-raised hover:border-base transition-colors duration-150 fg-dim"
                            :title="themeMode === 'light' ? 'Light \u2014 click for dark' : themeMode === 'dark' ? 'Dark \u2014 click for auto' : 'Auto (follows system) \u2014 click for light'"
                        >
                            <!-- Sun: light mode -->
                            <svg v-if="themeMode === 'light'" class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="1.75" viewBox="0 0 24 24">
                                <circle cx="12" cy="12" r="4" stroke-linecap="round"/>
                                <path stroke-linecap="round" d="M12 2v2M12 20v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M2 12h2M20 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42"/>
                            </svg>
                            <!-- Moon: dark mode -->
                            <svg v-else-if="themeMode === 'dark'" class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="1.75" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>
                            </svg>
                            <!-- Monitor: auto/system mode -->
                            <svg v-else class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="1.75" viewBox="0 0 24 24">
                                <rect x="2" y="3" width="20" height="14" rx="2" stroke-linecap="round" stroke-linejoin="round"/>
                                <path stroke-linecap="round" stroke-linejoin="round" d="M8 21h8M12 17v4"/>
                            </svg>
                        </button>
                        <form method="POST" action="/logout" style="margin:0">
                            <button type="submit" class="px-3 py-1.5 rounded font-mono text-xs fg-dim hover:text-red-400 hover:bg-raised border border-transparent hover:border-base transition-colors duration-150 uppercase tracking-widest">logout</button>
                        </form>
                    </div>
                </nav>
            `,
        });
    }

    global.registerSiteNavComponent = registerSiteNavComponent;
})(window);
