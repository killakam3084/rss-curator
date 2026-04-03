/**
 * auth-guard.js — Global session expiry handler.
 *
 * 1. Patches window.fetch so any 401 response from /api/* immediately
 *    redirects to /login?next=<current path>.
 *
 * 2. Exposes window.__authProbe() for EventSource error handlers, which
 *    cannot inspect HTTP status directly. Calling probe() fires a fetch to
 *    /api/stats; if the session is expired the patched fetch intercepts the
 *    401 and redirects. Probe calls are debounced to one per 3 seconds.
 */
(function (global) {
    const LOGIN_PATH = '/login';

    function redirectToLogin() {
        const next = encodeURIComponent(global.location.pathname + global.location.search);
        global.location.href = LOGIN_PATH + '?next=' + next;
    }

    // ── Patch window.fetch ────────────────────────────────────────────────
    const _fetch = global.fetch;
    global.fetch = function (...args) {
        return _fetch.apply(this, args).then(function (res) {
            if (res.status === 401) {
                redirectToLogin();
            }
            return res;
        });
    };

    // ── SSE probe helper ─────────────────────────────────────────────────
    // EventSource errors don't expose HTTP status, so callers probe a
    // protected endpoint; the patched fetch handles the 401 redirect.
    let _probeTimer = null;
    global.__authProbe = function () {
        if (_probeTimer !== null) return; // debounce
        _probeTimer = setTimeout(function () {
            _probeTimer = null;
            // Any protected endpoint works; /api/stats is lightweight.
            _fetch('/api/stats').then(function (res) {
                if (res.status === 401) redirectToLogin();
            }).catch(function () {}); // network error — leave reconnect to stream
        }, 3000);
    };
}(window));
