const { createApp, ref, reactive, computed, watch, onMounted } = Vue;

const settingsApp = createApp({
    setup() {

        // ── State ────────────────────────────────────────────────────
        const sections = [
            { id: 'scheduler',   label: 'scheduler'   },
            { id: 'auto_queue',  label: 'auto-queue'  },
            { id: 'alerts',      label: 'alerts'      },
            { id: 'match',       label: 'match'       },
            { id: 'plex',        label: 'plex'        },
            { id: 'auth',        label: 'auth'        },
        ];
        const activeSection = ref('scheduler');
        const loading = ref(true);
        const logsOpen = ref(false);
        const saving = ref(false);

        const feedCheckRunning = ref(false);  // true while polling on-demand feed-check job
        const autoQueueRunning = ref(false);  // true while polling on-demand auto-queue job

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
                dry_run: false,
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
            plex: {
                enabled: false,
                url: '',
                token: '***',
                section_keys: [],
                sync_enabled: false,
                sync_interval_secs: 21600,
                reconcile_on_feed_check: true,
            },
        });

        // Plex connection test + cached status
        const plexTesting = ref(false);
        const plexTestResult = ref(null);   // { ok, message }
        const plexLibraries = ref([]);      // sections reported by the server
        const plexStatus = ref(null);       // cached counts from /api/plex/status
        const plexSyncing = ref(false);
        // Separate token input so a blank field means "keep the stored token".
        const plexTokenInput = ref('');

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
                form.auto_queue.dry_run        = data.auto_queue.dry_run        ?? false;
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
            // plex — token always masked server-side, same as the password
            if (data.plex) {
                form.plex.enabled                 = data.plex.enabled                 ?? false;
                form.plex.url                     = data.plex.url                     ?? '';
                form.plex.section_keys            = data.plex.section_keys            ?? [];
                form.plex.sync_enabled            = data.plex.sync_enabled            ?? false;
                form.plex.sync_interval_secs      = data.plex.sync_interval_secs      ?? 21600;
                form.plex.reconcile_on_feed_check = data.plex.reconcile_on_feed_check ?? true;
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
            } else if (section === 'plex') {
                patch.plex = {
                    ...form.plex,
                    token: plexTokenInput.value.length > 0 ? plexTokenInput.value : '***',
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
                plexTokenInput.value = '';
                showToast('settings saved', 'success');
            } catch (err) {
                showToast(`save failed: ${err.message}`, 'error');
                console.error('save:', err);
            } finally {
                saving.value = false;
            }
        }

        // ── Plex ─────────────────────────────────────────────────────
        // Save before testing: the server tests with its stored credentials,
        // so an untested edit in the form would give a misleading result.
        async function testPlexConnection() {
            if (plexTesting.value) return;
            plexTesting.value = true;
            plexTestResult.value = null;
            try {
                await save('plex');
                const res = await fetch('/api/plex/meta');
                const data = await res.json().catch(() => ({}));
                if (!res.ok) {
                    plexTestResult.value = { ok: false, message: data.error || `HTTP ${res.status}` };
                    return;
                }
                plexLibraries.value = data.libraries || [];
                plexTestResult.value = data.connected
                    ? {
                        ok: true,
                        message: `connected to plex ${data.server_version || ''} — ${plexLibraries.value.length} usable librar${plexLibraries.value.length === 1 ? 'y' : 'ies'}`.trim(),
                    }
                    : { ok: false, message: data.error || 'could not reach plex' };
            } catch (err) {
                plexTestResult.value = { ok: false, message: err.message };
            } finally {
                plexTesting.value = false;
            }
        }

        async function loadPlexStatus() {
            try {
                const res = await fetch('/api/plex/status');
                if (!res.ok) return;
                plexStatus.value = await res.json();
            } catch (err) {
                console.error('loadPlexStatus:', err);
            }
        }

        async function runPlexSync() {
            if (plexSyncing.value) return;
            plexSyncing.value = true;
            try {
                const res = await fetch('/api/plex/sync', { method: 'POST' });
                if (res.status === 409) {
                    showToast('a plex sync is already running', 'error');
                    return;
                }
                if (!res.ok) {
                    const d = await res.json().catch(() => ({}));
                    showToast(d.error || `sync failed: HTTP ${res.status}`, 'error');
                    return;
                }
                showToast('plex sync started', 'success');
                // The job runs in the background; poll the cached counts for a
                // while so the panel reflects it without a manual reload.
                for (let i = 0; i < 15; i++) {
                    await new Promise(r => setTimeout(r, 2000));
                    await loadPlexStatus();
                }
            } catch (err) {
                showToast(`sync failed: ${err.message}`, 'error');
            } finally {
                plexSyncing.value = false;
            }
        }

        function togglePlexSection(key) {
            const keys = form.plex.section_keys;
            const idx = keys.indexOf(key);
            if (idx === -1) keys.push(key); else keys.splice(idx, 1);
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

        // ── Lifecycle ────────────────────────────────────────────────
        onMounted(() => {
            loadSettings();
            loadPlexStatus();
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
            feedCheckRunning,
            runFeedCheck,
            autoQueueRunning,
            runAutoQueue,
            plexTesting,
            plexTestResult,
            plexLibraries,
            plexStatus,
            plexSyncing,
            plexTokenInput,
            testPlexConnection,
            runPlexSync,
            togglePlexSection,
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
