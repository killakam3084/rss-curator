const { createApp, ref, reactive, computed, watch, onMounted } = Vue;

createApp({
    setup() {
        // ── Dark mode ────────────────────────────────────────────────
        const darkMode = ref(
            localStorage.getItem('rss-curator-dark-mode') === 'true' ||
            (localStorage.getItem('rss-curator-dark-mode') === null &&
                window.matchMedia('(prefers-color-scheme: dark)').matches)
        );

        // ── State ────────────────────────────────────────────────────
        const sections = [
            { id: 'scheduler', label: 'scheduler' },
            { id: 'alerts',    label: 'alerts'    },
            { id: 'match',     label: 'match'     },
            { id: 'auth',      label: 'auth'      },
            { id: 'shows',     label: 'shows'     },
        ];
        const activeSection = ref('scheduler');
        const loading = ref(true);
        const saving = ref(false);

        // ── Shows editor state ────────────────────────────────────────
        const showsSaving = ref(false);
        const showsCount  = ref(null); // null until first load
        const showsError  = ref('');
        let   showsCM     = null;      // CodeMirror instance (created lazily)

        // Flat form state mirroring AppSettings JSON shape
        const form = reactive({
            scheduler: {
                feed_check_interval_secs: 300,
                feed_check_enabled: true,
                rescore_backfill_enabled: false,
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
                showsCM.setSize('100%', '440px');
            } else {
                showsCM.setValue(value || '');
                showsCM.refresh();
            }
        }

        async function loadShows() {
            showsError.value = '';
            try {
                const res  = await fetch('/api/shows');
                if (!res.ok) throw new Error(`HTTP ${res.status}`);
                const data = await res.json();
                // data.shows_count exists; strip it before pretty-printing the config
                const { shows_count, ...cfg } = data;
                showsCount.value = shows_count ?? (data.shows ? data.shows.length : 0);
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
                showsError.value = `failed to load shows.json: ${err.message}`;
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
                const res = await fetch('/api/shows', {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(cfg),
                });
                const data = await res.json();
                if (!res.ok) {
                    showsError.value = data.error || `HTTP ${res.status}`;
                } else {
                    showsCount.value = data.shows_count ?? (data.shows ? data.shows.length : 0);
                    // Normalise editor content to what the server wrote
                    const { shows_count, ...saved } = data;
                    const pretty = JSON.stringify(saved, null, 2);
                    if (showsCM) showsCM.setValue(pretty);
                    showToast(`shows.json saved (${showsCount.value} show${showsCount.value !== 1 ? 's' : ''})`, 'success');
                }
            } catch (err) {
                showsError.value = `save failed: ${err.message}`;
                console.error('saveShows:', err);
            } finally {
                showsSaving.value = false;
            }
        }

        function formatShows() {
            showsError.value = '';
            const raw = showsCM ? showsCM.getValue() : '';
            try {
                const pretty = JSON.stringify(JSON.parse(raw), null, 2);
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
        // ── Lifecycle ────────────────────────────────────────────────
        onMounted(() => {
            document.documentElement.classList.toggle('dark', darkMode.value);
            loadSettings();
            loadShows();
        });

        // When the user navigates to the shows tab, lazily create the CM
        // editor (the #shows-editor div doesn't exist until the v-if renders).
        watch(activeSection, (section) => {
            if (section === 'shows') ensureShowsEditor();
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
            showsError,
            ensureShowsEditor,
            saveShows,
            formatShows,
            onShowsFileUpload,
        };
    }
}).mount('#settings-app');
