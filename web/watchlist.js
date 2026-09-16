const { createApp, ref, reactive, computed, watch, onMounted, nextTick } = Vue;

const watchlistApp = createApp({
    setup() {
        // ── View & Layout State ──────────────────────────────────────
        const editorMode = ref('visual'); // 'visual' | 'json'
        const loading = ref(true);
        const saving = ref(false);
        const logsOpen = ref(false);
        const enrichRunning = ref(false);
        const hasUnsavedChanges = ref(false);

        // ── Split & Suggestions Drawer State ─────────────────────────
        const suggestionsDrawerOpen = ref(false);
        const splitPercent = ref(38); // Suggestions panel width in %
        let isDraggingSplit = false;

        function toggleSuggestionsDrawer() {
            suggestionsDrawerOpen.value = !suggestionsDrawerOpen.value;
            if (suggestionsDrawerOpen.value) {
                loadSuggestStatus();
                loadCachedSuggestions();
            }
        }

        function startSplitDrag(e) {
            isDraggingSplit = true;
            document.body.style.cursor = 'col-resize';
            document.body.style.userSelect = 'none';
        }

        function onSplitDrag(e) {
            if (!isDraggingSplit) return;
            const container = document.getElementById('split-container');
            if (!container) return;
            const rect = container.getBoundingClientRect();
            const offsetRight = rect.right - e.clientX;
            const pct = Math.min(Math.max((offsetRight / rect.width) * 100, 20), 65);
            splitPercent.value = Math.round(pct);
            if (cmEditor) cmEditor.refresh();
        }

        function stopSplitDrag() {
            if (!isDraggingSplit) return;
            isDraggingSplit = false;
            document.body.style.cursor = '';
            document.body.style.userSelect = '';
        }

        // ── Watchlist Domain State ────────────────────────────────────
        const shows = ref([]);
        const movies = ref([]);

        // ── Visual Inspector Mode State ──────────────────────────────
        const visualFilterQuery = ref('');
        const visualContentType = ref('all'); // 'all' | 'shows' | 'movies'

        const filteredRules = computed(() => {
            const query = visualFilterQuery.value.trim().toLowerCase();
            const list = [];

            if (visualContentType.value === 'all' || visualContentType.value === 'shows') {
                for (const s of shows.value) {
                    if (!query || (s.name && s.name.toLowerCase().includes(query))) {
                        list.push({ type: 'show', rule: s });
                    }
                }
            }

            if (visualContentType.value === 'all' || visualContentType.value === 'movies') {
                for (const m of movies.value) {
                    if (!query || (m.name && m.name.toLowerCase().includes(query))) {
                        list.push({ type: 'movie', rule: m });
                    }
                }
            }

            return list;
        });

        // ── Add/Edit Modal State ──────────────────────────────────────
        const ruleModal = reactive({
            open: false,
            isEdit: false,
            originalType: 'show',
            originalName: '',
            form: {
                type: 'show',
                name: '',
                min_quality: '',
                preferred_codec: '',
                exclude_groups_str: '',
                preferred_groups_str: '',
            },
        });

        function parseCSV(str) {
            if (!str) return [];
            return str.split(',').map(s => s.trim()).filter(Boolean);
        }

        function openAddModal() {
            ruleModal.isEdit = false;
            ruleModal.originalType = 'show';
            ruleModal.originalName = '';
            ruleModal.form = {
                type: visualContentType.value === 'movies' ? 'movie' : 'show',
                name: '',
                min_quality: '',
                preferred_codec: '',
                exclude_groups_str: '',
                preferred_groups_str: '',
            };
            ruleModal.open = true;
        }

        function openEditModal(item) {
            ruleModal.isEdit = true;
            ruleModal.originalType = item.type;
            ruleModal.originalName = item.rule.name;
            ruleModal.form = {
                type: item.type,
                name: item.rule.name,
                min_quality: item.rule.min_quality || '',
                preferred_codec: item.rule.preferred_codec || '',
                exclude_groups_str: (item.rule.exclude_groups || []).join(', '),
                preferred_groups_str: (item.rule.preferred_groups || []).join(', '),
            };
            ruleModal.open = true;
        }

        function saveModalRule() {
            const name = ruleModal.form.name.trim();
            if (!name) {
                showToast('Name/title is required', 'error');
                return;
            }

            const ruleObj = { name };
            if (ruleModal.form.min_quality.trim()) {
                ruleObj.min_quality = ruleModal.form.min_quality.trim();
            }
            if (ruleModal.form.preferred_codec.trim()) {
                ruleObj.preferred_codec = ruleModal.form.preferred_codec.trim();
            }
            const exc = parseCSV(ruleModal.form.exclude_groups_str);
            if (exc.length > 0) ruleObj.exclude_groups = exc;
            const pref = parseCSV(ruleModal.form.preferred_groups_str);
            if (pref.length > 0) ruleObj.preferred_groups = pref;

            if (ruleModal.isEdit) {
                // Remove previous
                if (ruleModal.originalType === 'show') {
                    shows.value = shows.value.filter(s => s.name !== ruleModal.originalName);
                } else {
                    movies.value = movies.value.filter(m => m.name !== ruleModal.originalName);
                }
            } else {
                // Check duplicate
                const lc = name.toLowerCase();
                const exists = shows.value.some(s => s.name.toLowerCase() === lc) ||
                               movies.value.some(m => m.name.toLowerCase() === lc);
                if (exists) {
                    showToast(`"${name}" is already in the watchlist`, 'error');
                    return;
                }
            }

            // Insert new / updated
            if (ruleModal.form.type === 'show') {
                shows.value.push(ruleObj);
                shows.value.sort((a, b) => (a.name || '').localeCompare(b.name || ''));
            } else {
                movies.value.push(ruleObj);
                movies.value.sort((a, b) => (a.name || '').localeCompare(b.name || ''));
            }

            ruleModal.open = false;
            hasUnsavedChanges.value = true;
            syncToCodeMirror();
            showToast(`${ruleModal.isEdit ? 'Updated' : 'Added'} "${name}" — remember to save`, 'success');
        }

        function deleteRule(item) {
            if (item.type === 'show') {
                shows.value = shows.value.filter(s => s.name !== item.rule.name);
            } else {
                movies.value = movies.value.filter(m => m.name !== item.rule.name);
            }
            hasUnsavedChanges.value = true;
            syncToCodeMirror();
            showToast(`Removed "${item.rule.name}" — remember to save`, 'success');
        }

        // ── CodeMirror Raw JSON Mode ──────────────────────────────────
        let cmEditor = null;
        const jsonError = ref('');
        const jsonFilterQuery = ref('');
        let jsonFilterTimer = null;

        function initCodeMirror() {
            const el = document.getElementById('watchlist-cm-editor');
            if (!el || cmEditor) return;

            cmEditor = CodeMirror(el, {
                value: serializeWatchlist(),
                mode: { name: 'javascript', json: true },
                theme: 'material-darker',
                lineNumbers: true,
                tabSize: 2,
                indentWithTabs: false,
                lineWrapping: false,
                autofocus: false,
                extraKeys: {
                    'Ctrl-S': () => saveWatchlist(),
                    'Cmd-S':  () => saveWatchlist(),
                },
            });
            cmEditor.setSize('100%', '100%');

            cmEditor.on('change', () => {
                hasUnsavedChanges.value = true;
                syncFromCodeMirror();
            });
        }

        function serializeWatchlist() {
            const doc = {
                shows: shows.value,
                movies: movies.value,
            };
            return JSON.stringify(doc, null, 2);
        }

        function syncToCodeMirror() {
            if (!cmEditor) return;
            const curVal = cmEditor.getValue();
            const newVal = serializeWatchlist();
            if (curVal !== newVal) {
                const cursor = cmEditor.getCursor();
                const scrollInfo = cmEditor.getScrollInfo();
                cmEditor.setValue(newVal);
                cmEditor.setCursor(cursor);
                cmEditor.scrollTo(scrollInfo.left, scrollInfo.top);
            }
        }

        function syncFromCodeMirror() {
            if (!cmEditor) return;
            try {
                const parsed = JSON.parse(cmEditor.getValue());
                if (Array.isArray(parsed.shows)) shows.value = parsed.shows.filter(s => s && s.name);
                if (Array.isArray(parsed.movies)) movies.value = parsed.movies.filter(m => m && m.name);
                jsonError.value = '';
            } catch (err) {
                jsonError.value = `invalid JSON syntax: ${err.message}`;
            }
        }

        function setEditorMode(mode) {
            editorMode.value = mode;
            if (mode === 'json') {
                nextTick(() => {
                    initCodeMirror();
                    if (cmEditor) {
                        syncToCodeMirror();
                        cmEditor.refresh();
                    }
                });
            } else {
                // Leaving JSON mode: ensure parsed state is reflected in visual
                if (cmEditor) syncFromCodeMirror();
            }
        }

        function formatJsonEditor() {
            jsonError.value = '';
            const raw = cmEditor ? cmEditor.getValue() : serializeWatchlist();
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
                shows.value = parsed.shows || [];
                movies.value = parsed.movies || [];
                if (cmEditor) {
                    cmEditor.setValue(JSON.stringify(parsed, null, 2));
                    cmEditor.refresh();
                }
                hasUnsavedChanges.value = true;
                showToast('watchlist formatted & sorted', 'success');
            } catch (err) {
                jsonError.value = `invalid JSON — ${err.message}`;
            }
        }

        function onJsonFileUpload(event) {
            const file = event.target.files[0];
            if (!file) return;
            const reader = new FileReader();
            reader.onload = (e) => {
                jsonError.value = '';
                const text = e.target.result;
                try {
                    const parsed = JSON.parse(text);
                    shows.value = Array.isArray(parsed.shows) ? parsed.shows.filter(s => s && s.name) : [];
                    movies.value = Array.isArray(parsed.movies) ? parsed.movies.filter(m => m && m.name) : [];
                    syncToCodeMirror();
                    hasUnsavedChanges.value = true;
                    showToast('file loaded — review and save to apply', 'success');
                } catch (err) {
                    jsonError.value = `invalid JSON in uploaded file: ${err.message}`;
                }
            };
            reader.readAsText(file);
            event.target.value = '';
        }

        function onJsonFilter() {
            clearTimeout(jsonFilterTimer);
            const query = jsonFilterQuery.value.trim();
            if (!query) {
                if (cmEditor) cmEditor.execCommand('clearSearch');
                return;
            }
            if (query.length < 3) return;
            jsonFilterTimer = setTimeout(() => {
                if (!cmEditor) return;
                const doc = cmEditor.getDoc();
                const lineCount = doc.lineCount();
                const lcQuery = query.toLowerCase();
                for (let i = 0; i < lineCount; i++) {
                    const line = doc.getLine(i);
                    if (!line) continue;
                    const lcLine = line.toLowerCase();
                    if (!lcLine.includes('"name"') || !lcLine.includes(lcQuery)) continue;
                    const matchIdx = lcLine.indexOf(lcQuery);
                    const from = { line: i, ch: matchIdx };
                    const to   = { line: i, ch: matchIdx + query.length };
                    cmEditor.scrollIntoView({ line: i, ch: matchIdx }, 80);
                    doc.setSelection(from, to, { scroll: false });
                    return;
                }
            }, 300);
        }

        function focusJsonEditor() {
            if (cmEditor) cmEditor.focus();
        }

        // ── Load & Save API ───────────────────────────────────────────
        async function loadWatchlist() {
            loading.value = true;
            try {
                const res = await fetch('/api/watchlist');
                if (!res.ok) throw new Error(`HTTP ${res.status}`);
                const data = await res.json();
                shows.value = Array.isArray(data.shows) ? data.shows.filter(s => s && s.name) : [];
                movies.value = Array.isArray(data.movies) ? data.movies.filter(m => m && m.name) : [];
                hasUnsavedChanges.value = false;
            } catch (err) {
                showToast(`failed to load watchlist: ${err.message}`, 'error');
                console.error('loadWatchlist:', err);
            } finally {
                loading.value = false;
            }
        }

        async function saveWatchlist() {
            let payload;
            if (editorMode.value === 'json' && cmEditor) {
                try {
                    payload = JSON.parse(cmEditor.getValue());
                } catch (err) {
                    jsonError.value = `invalid JSON — ${err.message}`;
                    showToast('Cannot save invalid JSON', 'error');
                    return;
                }
            } else {
                payload = {
                    shows: shows.value,
                    movies: movies.value,
                };
            }

            saving.value = true;
            try {
                const res = await fetch('/api/watchlist', {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload),
                });
                const data = await res.json();
                if (!res.ok) {
                    showToast(data.error || `HTTP ${res.status}`, 'error');
                } else {
                    shows.value = Array.isArray(data.shows) ? data.shows.filter(s => s && s.name) : [];
                    movies.value = Array.isArray(data.movies) ? data.movies.filter(m => m && m.name) : [];
                    syncToCodeMirror();
                    hasUnsavedChanges.value = false;
                    const sl = shows.value.length;
                    const ml = movies.value.length;
                    const parts = [`${sl} show${sl !== 1 ? 's' : ''}`];
                    if (ml > 0) parts.push(`${ml} movie${ml !== 1 ? 's' : ''}`);
                    showToast(`watchlist saved (${parts.join(', ')})`, 'success');
                }
            } catch (err) {
                showToast(`save failed: ${err.message}`, 'error');
                console.error('saveWatchlist:', err);
            } finally {
                saving.value = false;
            }
        }

        // ── Suggestions API ───────────────────────────────────────────
        const suggestAvailable   = ref(false);
        const suggestShowsCount  = ref(null);
        const suggestMoviesCount = ref(0);
        const suggestions        = ref([]);
        const suggestsLoading    = ref(false);
        const suggestError       = ref('');
        const suggestGeneratedAt = ref(null);
        const suggestRefreshing  = ref(false);
        const suggestActiveCount = ref(0);
        const suggestActiveLimit = ref(0);

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
                if (res.status === 503) return; // AI not configured
                const data = await res.json();
                if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
                suggestions.value = data.suggestions || [];
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
                // Poll until terminal state
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
                            await loadSuggestStatus();
                        } else if (job.status === 'failed' || job.status === 'cancelled') {
                            done = true;
                            suggestError.value = `refresh ${job.status}${job.error ? ': ' + job.error : ''}`;
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

        function addSuggestionToWatchlist(suggestion) {
            const isMovie = suggestion.content_type === 'movie';
            const lcName = (suggestion.show_name || '').toLowerCase();

            // Duplicate check
            const inShows = shows.value.some(s => (s.name || '').toLowerCase() === lcName);
            const inMovies = movies.value.some(m => (m.name || '').toLowerCase() === lcName);
            if (inShows || inMovies) {
                showToast(`"${suggestion.show_name}" is already in the watchlist`, 'error');
                return;
            }

            const ruleObj = suggestion.rule || { name: suggestion.show_name };
            if (isMovie) {
                movies.value.push(ruleObj);
                movies.value.sort((a, b) => (a.name || '').localeCompare(b.name || ''));
            } else {
                shows.value.push(ruleObj);
                shows.value.sort((a, b) => (a.name || '').localeCompare(b.name || ''));
            }

            hasUnsavedChanges.value = true;
            syncToCodeMirror();

            // Optimistically remove and persist dismissal
            suggestions.value = suggestions.value.filter(s => s.show_name !== suggestion.show_name);
            fetch('/api/suggestions/dismiss', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ show_name: suggestion.show_name }),
            }).catch(err => console.error('addSuggestion: dismiss failed', err));

            showToast(`Added "${suggestion.show_name}" to ${isMovie ? 'movies' : 'shows'} — remember to save`, 'success');
        }

        async function dismissSuggestion(suggestion) {
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

        async function runWatchlistEnrich() {
            if (enrichRunning.value) return;
            enrichRunning.value = true;
            try {
                const res = await fetch('/api/scheduler/run/watchlist_enrich', { method: 'POST' });
                if (res.status === 409) {
                    showToast('Watchlist enrich is already running', 'error');
                    return;
                }
                if (!res.ok) {
                    const d = await res.json().catch(() => ({}));
                    showToast(`Enrich failed: ${d.error || 'HTTP ' + res.status}`, 'error');
                    return;
                }
                showToast('Watchlist enrich triggered', 'success');
            } catch (err) {
                showToast(`Enrich error: ${err.message}`, 'error');
            } finally {
                setTimeout(() => { enrichRunning.value = false; }, 3000);
            }
        }

        // ── Toast Notification ────────────────────────────────────────
        const toast = reactive({ visible: false, message: '', type: 'success' });
        let toastTimer = null;

        function showToast(message, type = 'success') {
            clearTimeout(toastTimer);
            toast.message = message;
            toast.type = type;
            toast.visible = true;
            toastTimer = setTimeout(() => { toast.visible = false; }, 3000);
        }

        // ── Lifecycle ─────────────────────────────────────────────────
        onMounted(async () => {
            await loadWatchlist();
            loadSuggestStatus();
            loadCachedSuggestions();
        });

        return {
            editorMode,
            loading,
            saving,
            logsOpen,
            enrichRunning,
            hasUnsavedChanges,
            suggestionsDrawerOpen,
            splitPercent,
            toggleSuggestionsDrawer,
            startSplitDrag,
            onSplitDrag,
            stopSplitDrag,
            // Watchlist
            shows,
            movies,
            visualFilterQuery,
            visualContentType,
            filteredRules,
            ruleModal,
            openAddModal,
            openEditModal,
            saveModalRule,
            deleteRule,
            setEditorMode,
            saveWatchlist,
            // CodeMirror JSON
            jsonError,
            jsonFilterQuery,
            onJsonFilter,
            focusJsonEditor,
            formatJsonEditor,
            onJsonFileUpload,
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
            refreshSuggestions,
            addSuggestionToWatchlist,
            dismissSuggestion,
            runWatchlistEnrich,
            toast,
        };
    }
});

// Register shared components
if (window.registerCuratorBtnComponent) {
    window.registerCuratorBtnComponent(watchlistApp);
}
if (window.registerSiteNavComponent) {
    window.registerSiteNavComponent(watchlistApp);
}
if (window.registerLogViewerComponent) {
    window.registerLogViewerComponent(watchlistApp);
}

watchlistApp.mount('#watchlist-app');