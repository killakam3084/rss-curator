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

                const darkMode = ref(false);

                const applyDarkMode = () => {
                    document.documentElement.classList.toggle('dark', darkMode.value);
                };

                const toggleDarkMode = () => {
                    darkMode.value = !darkMode.value;
                    applyDarkMode();
                    localStorage.setItem('rss-curator-dark-mode', JSON.stringify(darkMode.value));
                };

                onMounted(() => {
                    const saved = localStorage.getItem('rss-curator-dark-mode');
                    darkMode.value = saved === 'true' ||
                        (saved === null && window.matchMedia('(prefers-color-scheme: dark)').matches);
                    applyDarkMode();

                    // Follow system preference changes only when the user has not set a manual override.
                    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (e) => {
                        if (localStorage.getItem('rss-curator-dark-mode') === null) {
                            darkMode.value = e.matches;
                            applyDarkMode();
                        }
                    });
                });

                return { darkMode, toggleDarkMode };
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
                            @click="toggleDarkMode"
                            class="p-2 rounded border border-transparent hover:bg-raised hover:border-base transition-colors duration-150"
                            :title="darkMode ? 'Switch to light mode' : 'Switch to dark mode'"
                        >
                            <span class="text-base leading-none select-none">{{ darkMode ? '☀️' : '🌙' }}</span>
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
