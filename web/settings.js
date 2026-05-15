const { createApp, ref, reactive, computed, watch, onMounted } = Vue;

const settingsApp = createApp({
    setup() {

        // ── State ────────────────────────────────────────────────────
        const sections = [
            { id: 'scheduler',   label: 'scheduler'   },
            { id: 'auto_queue',  label: 'auto-queue'  },
            { id: 'alerts',      label: 'alerts'      },
            { id: 'match',       label: 'match'       },
            { id: 'auth',        label: 'auth'        },
            { id: 'watchlist',   label: 'watchlist'   },
            { id: 'suggestions', label: 'suggestions' },
        ];
        const activeSection = ref('scheduler');
        const loading = ref(true);
        const logsOpen = ref(false);
        const saving = ref(false);

        // ── Shows editor state ────────────────────────────────────────
        const showsSaving     = ref(false);
        const showsCount      = ref(null); // null until first load
        const moviesCount     = ref(null); // null until first load
        const showsError      = ref('');
        const watchlistFilter = ref('');
        let   showsCM         = null;      // CodeMirror instance (created lazily)

        // ── Suggestions state ─────────────────────────────────────────
        const suggestAvailable   = ref(false);
        const suggestShowsCount  = ref(null);   // from /api/suggestions/status
        const suggestMoviesCount = ref(0);
        const suggestions        = ref([]);
        const suggestsLoading    = ref(false);
        const suggestError       = ref('');
        const suggestGeneratedAt = ref(null);   // ISO string from cache
        const suggestRefreshing  = ref(false);  // true while polling refresh job
        const suggestActiveCount = ref(0);      // current active row count
        const suggestActiveLimit = ref(0);      // cap configured on server (CURATOR_SUGGESTIONS_LIMIT)
        const feedCheckRunning        = ref(false);  // true while polling on-demand feed-check job
        const watchlistEnrichRunning = ref(false);  // true briefly after triggering watchlist_enrich
        const autoQueueRunning       = ref(false);  // true while polling on-demand auto-queue job

        // Flat form state mirroring AppSettings JSON shape
        const form = reactive({
            scheduler: {
                feed_check_interval_secs: 300,
                feed_check_enabled: true,
                rescore_backfill_enabled: false,
            },
            auto_queue: {
                enabled: false,
                min_ai_score: 0.80,
                min_confidence: 0.85,
                interval_secs: 600,
                hold_mins: 30,
                max_hold_mins: 480,
            },
            alerts: {
                alert_poller_interval_secs: 60,
                progress_interval: 300,
            },
            match: {
                min_quality: '',
                preferred_codec: '',
                exclude_groups: [],
                preferred_groups: [],
            },
            auth: {
                username: '',
                password: '***',
            },
        });

        // Comma-separated text inputs for array fields
        const preferredGroupsInput = ref('');
        const excludeGroupsInput   = ref('');
        // Separate password input so we can send sentinel when blank
        const passwordInput = ref('');

        // ── Toast ────────────────────────────────────────────────────
        const toast = reactive({ visible: false, message: '', type: 'success' });
        let toastTimer = null;

        function showToast(message, type = 'success') {
            clearTimeout(toastTimer);
            toast.message = message;
            toast.type = type;
            toast.visible = true;
            toastTimer = setTimeout(() => { toast.visible = false; }, 3000);
        }

        // ── Helpers ──────────────────────────────────────────────────
        function parseCSV(str) {
            return str.split(',').map(s => s.trim()).filter(Boolean);
        }

        function populateForm(data) {
            // scheduler
            if (data.scheduler) {
                form.scheduler.feed_check_interval_secs  = data.scheduler.feed_check_interval_secs  ?? 300;
                form.scheduler.feed_check_enabled        = data.scheduler.feed_check_enabled        ?? true;
                form.scheduler.rescore_backfill_enabled  = data.scheduler.rescore_backfill_enabled  ?? false;
            }
            // auto_queue
            if (data.auto_queue) {
                form.auto_queue.enabled        = data.auto_queue.enabled        ?? false;
                form.auto_queue.min_ai_score   = data.auto_queue.min_ai_score   ?? 0.80;
                form.auto_queue.min_confidence = data.auto_queue.min_confidence ?? 0.85;
                form.auto_queue.interval_secs  = data.auto_queue.interval_secs  ?? 600;
                form.auto_queue.hold_mins      = data.auto_queue.hold_mins      ?? 30;
                form.auto_queue.max_hold_mins  = data.auto_queue.max_hold_mins  ?? 480;
            }
            // alerts
            if (data.alerts) {
                form.alerts.alert_poller_interval_secs = data.alerts.alert_poller_interval_secs ?? 60;
                form.alerts.progress_interval          = data.alerts.progress_interval          ?? 300;
            }
            // match
            if (data.match) {
                form.match.min_quality       = data.match.min_quality       ?? '';
                form.match.preferred_codec   = data.match.preferred_codec   ?? '';
                form.match.exclude_groups    = data.match.exclude_groups    ?? [];
                form.match.preferred_groups  = data.match.preferred_groups  ?? [];
                preferredGroupsInput.value   = (form.match.preferred_groups).join(', ');
                excludeGroupsInput.value     = (form.match.exclude_groups).join(', ');
            }
            // auth — password always masked server-side
            if (data.auth) {
                form.auth.username = data.auth.username ?? '';
                // passwordInput stays empty; user types new value if they want to change it
            }
        }

        // ── Load ─────────────────────────────────────────────────────
        async function loadSettings() {
            loading.value = true;
            try {
                const res = await fetch('/api/settings');
                if (!res.ok) throw new Error(`HTTP ${res.status}`);
                const data = await res.json();
                populateForm(data);
            } catch (err) {
                showToast('failed to load settings', 'error');
                console.error('loadSettings:', err);
            } finally {
                loading.value = false;
            }
        }

        // ── Save ─────────────────────────────────────────────────────
        async function save(section) {
            saving.value = true;

            // Sync array fields from their text inputs right before sending
            if (section === 'match') {
                form.match.preferred_groups = parseCSV(preferredGroupsInput.value);
                form.match.exclude_groups   = parseCSV(excludeGroupsInput.value);
            }

            // Build patch payload — send only the section being saved so we
            // don't accidentally overwrite other settings with stale UI state.
            // The server accepts a partial AppSettings object; unset fields are
            // treated as zero-value and skipped during merge on the backend.
            const patch = {};
            if (section === 'scheduler') {
                patch.scheduler = { ...form.scheduler };
            } else if (section === 'auto_queue') {
                patch.auto_queue = { ...form.auto_queue };
            } else if (section === 'alerts') {
                patch.alerts = { ...form.alerts };
            } else if (section === 'match') {
                patch.match = { ...form.match };
            } else if (section === 'auth') {
                patch.auth = {
                    username: form.auth.username,
                    // Send actual value if the user typed something; otherwise
                    // send sentinel so the server keeps the current password.
                    password: passwordInput.value.length > 0 ? passwordInput.value : '***',
                };
            }

            try {
                const res = await fetch('/api/settings', {
                    method: 'PATCH',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(patch),
                });
                if (!res.ok) {
                    const text = await res.text();
                    throw new Error(text || `HTTP ${res.status}`);
                }
                const updated = await res.json();
                populateForm(updated);
                passwordInput.value = ''; // clear after successful save
                showToast('settings saved', 'success');
            } catch (err) {
                showToast(`save failed: ${err.message}`, 'error');
                console.error('save:', err);
            } finally {
                saving.value = false;
            }
        }
        // ── Shows editor helpers ──────────────────────────────────

        // Initialise (first time) or refresh (tab switch) the CodeMirror editor.
        // Must be called after the #shows-editor div is in the DOM.
        function initOrRefreshShowsCM(value) {
            const el = document.getElementById('shows-editor');
            if (!el) return;
            if (!showsCM) {
                showsCM = CodeMirror(el, {
                    value: value || '',
                    mode: { name: 'javascript', json: true },
                    theme: 'material-darker',
                    lineNumbers: true,
                    tabSize: 2,
                    indentWithTabs: false,
                    lineWrapping: false,
                    autofocus: true,
                    extraKeys: {
                        'Ctrl-S': () => saveShows(),
                        'Cmd-S':  () => saveShows(),
                    },
                });
                // Fix height to fill container div
                showsCM.setSize('100%', 'calc(100vh - 280px)');
            } else {
                showsCM.setValue(value || '');
                showsCM.refresh();
            }
        }

        async function loadShows() {
            showsError.value = '';
            try {
                const res  = await fetch('/api/watchlist');
                if (!res.ok) throw new Error(`HTTP ${res.status}`);
                const data = await res.json();
                // data.shows_count and data.movies_count exist; strip before pretty-printing
                const { shows_count, movies_count, ...cfg } = data;
                showsCount.value  = shows_count  ?? (data.shows  ? data.shows.length  : 0);
                moviesCount.value = movies_count ?? (data.movies ? data.movies.length : 0);
                const pretty = JSON.stringify(cfg, null, 2);
                // Editor may not be in DOM yet if tab hasn't been opened — store for later
                if (showsCM) {
                    showsCM.setValue(pretty);
                    showsCM.refresh();
                } else {
                    // Will be picked up by the watch on activeSection
                    pendingShowsValue = pretty;
                }
            } catch (err) {
                showsError.value = `failed to load watchlist.json: ${err.message}`;
                console.error('loadShows:', err);
            }
        }

        // Holds the value to seed the editor with before it has been created.
        let pendingShowsValue = null;

        function ensureShowsEditor() {
            // nextTick alternative: queue a microtask so the v-if DOM is rendered
            Promise.resolve().then(() => {
                initOrRefreshShowsCM(pendingShowsValue || (showsCM ? showsCM.getValue() : ''));
                pendingShowsValue = null;
            });
        }

        async function saveShows() {
            showsError.value = '';
            const raw = showsCM ? showsCM.getValue() : '';
            let cfg;
            try {
                cfg = JSON.parse(raw);
            } catch (err) {
                showsError.value = `invalid JSON — ${err.message}`;
                return;
            }
            showsSaving.value = true;
            try {
                const res = await fetch('/api/watchlist', {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(cfg),
                });
                const data = await res.json();
                if (!res.ok) {
                    showsError.value = data.error || `HTTP ${res.status}`;
                } else {
                    showsCount.value  = data.shows_count  ?? (data.shows  ? data.shows.length  : 0);
                    moviesCount.value = data.movies_count ?? (data.movies ? data.movies.length : 0);
                    // Normalise editor content to what the server wrote
                    const { shows_count, movies_count, ...saved } = data;
                    const pretty = JSON.stringify(saved, null, 2);
                    if (showsCM) showsCM.setValue(pretty);
                    const sl = showsCount.value;
                    const ml = moviesCount.value;
                    const parts = [`${sl} show${sl !== 1 ? 's' : ''}`];
                    if (ml > 0) parts.push(`${ml} movie${ml !== 1 ? 's' : ''}`);
                    showToast(`watchlist saved (${parts.join(', ')})`, 'success');
                }
            } catch (err) {
                showsError.value = `save failed: ${err.message}`;
                console.error('saveShows:', err);
            } finally {
                showsSaving.value = false;
            }
        }

        let _watchlistFilterTimer = null;

        function onWatchlistFilter() {
            clearTimeout(_watchlistFilterTimer);
            const query = watchlistFilter.value.trim();
            if (!query) {
                // Immediate clear — no debounce needed.
                if (showsCM) showsCM.execCommand('clearSearch');
                return;
            }
            // Require at least 3 characters before jumping, debounced 300ms.
            if (query.length < 3) return;
            _watchlistFilterTimer = setTimeout(() => {
                if (!showsCM) return;
                const cm = showsCM;
                const doc = cm.getDoc();
                const lineCount = doc.lineCount();
                const lcQuery = query.toLowerCase();
                for (let i = 0; i < lineCount; i++) {
                    const line = doc.getLine(i);
                    if (!line) continue;
                    const lcLine = line.toLowerCase();
                    if (!lcLine.includes('"name"') || !lcLine.includes(lcQuery)) continue;
                    // Find the start/end of the matched name value within the line
                    // so we can select it rather than drop a bare cursor.
                    const matchIdx = lcLine.indexOf(lcQuery);
                    const from = { line: i, ch: matchIdx };
                    const to   = { line: i, ch: matchIdx + query.length };
                    cm.scrollIntoView({ line: i, ch: matchIdx }, 80);
                    doc.setSelection(from, to, { scroll: false });
                    return;
                }
            }, 300);
        }

        function focusWatchlistEditor() {
            if (showsCM) showsCM.focus();
        }

        function formatShows() {
            showsError.value = '';
            const raw = showsCM ? showsCM.getValue() : '';
            try {
                const parsed = JSON.parse(raw);
                if (Array.isArray(parsed.shows)) {
                    parsed.shows.sort((a, b) =>
                        (a.name || '').localeCompare(b.name || '', undefined, { sensitivity: 'base' })
                    );
                }
                if (Array.isArray(parsed.movies)) {
                    parsed.movies.sort((a, b) =>
                        (a.name || '').localeCompare(b.name || '', undefined, { sensitivity: 'base' })
                    );
                }
                const pretty = JSON.stringify(parsed, null, 2);
                if (showsCM) showsCM.setValue(pretty);
            } catch (err) {
                showsError.value = `invalid JSON — ${err.message}`;
            }
        }

        function onShowsFileUpload(event) {
            const file = event.target.files[0];
            if (!file) return;
            const reader = new FileReader();
            reader.onload = (e) => {
                showsError.value = '';
                const text = e.target.result;
                try {
                    // Validate and reformat
                    const pretty = JSON.stringify(JSON.parse(text), null, 2);
                    if (showsCM) showsCM.setValue(pretty);
                    showToast('file loaded — review and save to apply', 'success');
                } catch (err) {
                    showsError.value = `invalid JSON in uploaded file: ${err.message}`;
                }
            };
            reader.readAsText(file);
            // Reset input so the same file can be re-uploaded
            event.target.value = '';
        }
        // ── Suggestions helpers ───────────────────────────────────────

        async function loadSuggestStatus() {
            try {
                const res = await fetch('/api/suggestions/status');
                if (!res.ok) return;
                const data = await res.json();
                suggestAvailable.value   = data.available ?? false;
                suggestShowsCount.value  = data.shows_count  ?? null;
                suggestMoviesCount.value = data.movies_count ?? 0;
                suggestActiveCount.value = data.active_count ?? 0;
                suggestActiveLimit.value = data.active_limit ?? 0;
            } catch (e) {
                suggestAvailable.value = false;
            }
        }

        async function loadCachedSuggestions() {
            suggestsLoading.value = true;
            suggestError.value = '';
            try {
                const res = await fetch('/api/suggestions');
                const data = await res.json();
                if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
                suggestions.value = data.suggestions || [];
                // Derive generated_at from the most-recent row (rows are ordered DESC).
                if (suggestions.value.length > 0) suggestGeneratedAt.value = suggestions.value[0].generated_at;
            } catch (err) {
                suggestError.value = `could not load suggestions: ${err.message}`;
                console.error('loadCachedSuggestions:', err);
            } finally {
                suggestsLoading.value = false;
            }
        }

        async function refreshSuggestions() {
            if (suggestRefreshing.value) return;
            suggestRefreshing.value = true;
            suggestError.value = '';
            try {
                const res = await fetch('/api/suggestions/refresh', { method: 'POST' });
                if (res.status === 409) {
                    suggestError.value = 'refresh already running — check back in a moment';
                    return;
                }
                if (res.status === 503) {
                    suggestError.value = 'AI provider unavailable';
                    return;
                }
                if (!res.ok) {
                    const d = await res.json().catch(() => ({}));
                    suggestError.value = `refresh failed: ${d.error || 'HTTP ' + res.status}`;
                    return;
                }
                const { job_id } = await res.json();
                // Poll until terminal state.
                let done = false;
                while (!done) {
                    await new Promise(r => setTimeout(r, 3000));
                    try {
                        const jr = await fetch(`/api/jobs/${job_id}`);
                        if (!jr.ok) break;
                        const job = await jr.json();
                        if (job.status === 'completed') {
                            done = true;
                            await loadCachedSuggestions();
                        } else if (job.status === 'failed' || job.status === 'cancelled') {
                            done = true;
                            suggestError.value = `refresh ${job.status}${ job.error ? ': ' + job.error : '' }`;
                        }
                    } catch (e) {
                        break;
                    }
                }
            } catch (err) {
                suggestError.value = `refresh failed: ${err.message}`;
                console.error('refreshSuggestions:', err);
            } finally {
                suggestRefreshing.value = false;
            }
        }

        async function runAutoQueue() {
            if (autoQueueRunning.value) return;
            autoQueueRunning.value = true;
            try {
                const res = await fetch('/api/auto-queue', { method: 'POST' });
                if (res.status === 409) return; // already running
                if (res.status === 503) {
                    console.warn('runAutoQueue: service unavailable');
                    return;
                }
                if (!res.ok) {
                    const d = await res.json().catch(() => ({}));
                    console.error('runAutoQueue failed:', d.error || 'HTTP ' + res.status);
                    return;
                }
            } catch (err) {
                console.error('runAutoQueue:', err);
            } finally {
                setTimeout(() => { autoQueueRunning.value = false; }, 3000);
            }
        }

        async function runFeedCheck() {
            if (feedCheckRunning.value) return;
            feedCheckRunning.value = true;
            try {
                const res = await fetch('/api/feed-check', { method: 'POST' });
                if (res.status === 409) {
                    // Already running — nothing to do, will finish on its own.
                    return;
                }
                if (res.status === 503) {
                    console.warn('runFeedCheck: job queue unavailable');
                    return;
                }
                if (!res.ok) {
                    const d = await res.json().catch(() => ({}));
                    console.error('runFeedCheck failed:', d.error || 'HTTP ' + res.status);
                    return;
                }
                const { job_id } = await res.json();
                // Poll until terminal state.
                let done = false;
                while (!done) {
                    await new Promise(r => setTimeout(r, 2000));
                    try {
                        const jr = await fetch(`/api/jobs/${job_id}`);
                        if (!jr.ok) break;
                        const job = await jr.json();
                        if (job.status === 'completed' || job.status === 'failed' || job.status === 'cancelled') {
                            done = true;
                        }
                    } catch (e) {
                        break;
                    }
                }
            } catch (err) {
                console.error('runFeedCheck:', err);
            } finally {
                feedCheckRunning.value = false;
            }
        }

        async function runWatchlistEnrich() {
            if (watchlistEnrichRunning.value) return;
            watchlistEnrichRunning.value = true;
            try {
                const res = await fetch('/api/scheduler/run/watchlist_enrich', { method: 'POST' });
                if (res.status === 409) {
                    console.warn('runWatchlistEnrich: already running');
                    return;
                }
                if (!res.ok) {
                    const d = await res.json().catch(() => ({}));
                    console.error('runWatchlistEnrich failed:', d.error || 'HTTP ' + res.status);
                    return;
                }
            } catch (err) {
                console.error('runWatchlistEnrich:', err);
            } finally {
                setTimeout(() => { watchlistEnrichRunning.value = false; }, 3000);
            }
        }

        function addSuggestion(suggestion) {
            // If the user is on the suggestions section, navigate to watchlist first
            // so the editor is initialised before we try to inject into it.
            if (activeSection.value !== 'watchlist') {
                activeSection.value = 'watchlist';
                setTimeout(() => addSuggestion(suggestion), 0);
                return;
            }
            if (!showsCM) return;
            const raw = showsCM.getValue();
            let cfg;
            try {
                cfg = JSON.parse(raw);
            } catch (e) {
                showsError.value = `invalid JSON — ${e.message}`;
                return;
            }
            const isMovie = suggestion.content_type === 'movie';
            if (!Array.isArray(cfg.shows))  cfg.shows  = [];
            if (!Array.isArray(cfg.movies)) cfg.movies = [];
            // Strip any null / empty-name entries left by prior bugs.
            cfg.shows  = cfg.shows.filter(s  => s && s.name);
            cfg.movies = cfg.movies.filter(m => m && m.name);
            // Check for duplicates in both arrays.
            const lcName = suggestion.show_name.toLowerCase();
            const alreadyInShows  = cfg.shows.some(s  => s.name.toLowerCase() === lcName);
            const alreadyInMovies = cfg.movies.some(m => m.name.toLowerCase() === lcName);
            if (alreadyInShows || alreadyInMovies) {
                showToast(`"${suggestion.show_name}" is already in the watchlist`, 'error');
                return;
            }
            // suggestion.rule is the stored ShowRule/MovieRule object (from rule_json).
            if (isMovie) {
                cfg.movies.push(suggestion.rule);
                cfg.movies.sort((a, b) => a.name.localeCompare(b.name));
            } else {
                cfg.shows.push(suggestion.rule);
                cfg.shows.sort((a, b) => a.name.localeCompare(b.name));
            }
            showsCM.setValue(JSON.stringify(cfg, null, 2));
            showsCM.refresh();
            // Remove from suggestions list so the row disappears immediately,
            // then persist via dismiss so it stays gone on the next fetch.
            suggestions.value = suggestions.value.filter(s => s.show_name !== suggestion.show_name);
            fetch('/api/suggestions/dismiss', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ show_name: suggestion.show_name }),
            }).catch(err => console.error('addSuggestion: dismiss failed', err));
            showToast(`added "${suggestion.show_name}" to ${isMovie ? 'movies' : 'shows'} — remember to save`, 'success');
        }

        async function dismissSuggestion(suggestion) {
            // Optimistic removal — no spinner needed.
            suggestions.value = suggestions.value.filter(s => s.show_name !== suggestion.show_name);
            try {
                await fetch('/api/suggestions/dismiss', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ show_name: suggestion.show_name }),
                });
            } catch (err) {
                console.error('dismissSuggestion:', err);
            }
        }

        // ── Lifecycle ────────────────────────────────────────────────
        onMounted(() => {
            loadSettings();
            loadShows();
        });

        // When the user navigates to the shows tab, lazily create the CM
        // editor (the #shows-editor div doesn't exist until the v-if renders).
        // When leaving, v-if destroys the div — null showsCM so the next visit
        // recreates it in the fresh element rather than calling into a detached node.
        watch(activeSection, (newSection, oldSection) => {
            if (oldSection === 'watchlist' && showsCM) {
                pendingShowsValue = showsCM.getValue();
                showsCM = null;
                watchlistFilter.value = '';
                clearTimeout(_watchlistFilterTimer);
            }
            if (newSection === 'watchlist') {
                ensureShowsEditor();
            }
            if (newSection === 'suggestions' || newSection === 'watchlist') {
                loadSuggestStatus();
                loadCachedSuggestions();
            }
        });

        return {
            sections,
            activeSection,
            loading,
            saving,
            form,
            preferredGroupsInput,
            excludeGroupsInput,
            passwordInput,
            toast,
            save,
            // Shows tab
            showsSaving,
            showsCount,
            moviesCount,
            showsError,
            watchlistFilter,
            onWatchlistFilter,
            focusWatchlistEditor,
            ensureShowsEditor,
            saveShows,
            formatShows,
            onShowsFileUpload,
            // Suggestions
            suggestAvailable,
            suggestShowsCount,
            suggestMoviesCount,
            suggestActiveCount,
            suggestActiveLimit,
            suggestions,
            suggestsLoading,
            suggestError,
            suggestGeneratedAt,
            suggestRefreshing,
            loadCachedSuggestions,
            refreshSuggestions,
            feedCheckRunning,
            runFeedCheck,
            watchlistEnrichRunning,
            runWatchlistEnrich,
            autoQueueRunning,
            runAutoQueue,
            addSuggestion,
            dismissSuggestion,
            logsOpen,
        };
    }
});

if (window.registerCuratorBtnComponent) {
    window.registerCuratorBtnComponent(settingsApp);
}
if (window.registerSiteNavComponent) {
    window.registerSiteNavComponent(settingsApp);
}
if (window.registerLogViewerComponent) {
    window.registerLogViewerComponent(settingsApp);
}

settingsApp.mount('#settings-app');
