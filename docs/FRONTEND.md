# Frontend Architecture

Overview of the rss-curator web UI: stack choices, component model, design token system,
and conventions for future contributors.

---

## Stack

| Concern | Choice | Notes |
|---------|--------|-------|
| Framework | Vue 3 (Options / Composition API) | Loaded via CDN — no build step, no bundler |
| Styling | Tailwind CSS v3 (CDN) | `darkMode: 'class'`, custom `curator` color palette |
| Design tokens | CSS custom properties (`web/style.css`) | Bare RGB triplets for Tailwind opacity modifier compatibility |
| Icons | Heroicons inline SVG | Inlined directly in component templates |
| Code editor | CodeMirror 5 (CDN) | Used only for the `shows.json` editor in Settings |

No `package.json`, no compile step. To develop locally: open any HTML file in a browser
or serve the `web/` directory with any static file server.

---

## File Layout

```
web/
├── index.html          # Main torrent dashboard
├── jobs.html           # Async jobs monitor
├── settings.html       # App configuration
├── login.html          # Login page
├── app.js              # Root Vue app for index.html
├── settings.js         # Root Vue app for settings.html
├── style.css           # Design tokens + semantic utility classes
└── components/
    ├── auth-guard.js   # fetch() 401 interceptor (utility, not a Vue component)
    ├── site-nav.js     # Primary navigation bar + theme toggle
    ├── app-sidebar.js  # Right-hand console sidebar
    ├── torrent-card.js # Individual torrent row card
    ├── ops-banner.js   # In-progress job progress banner
    ├── jobs-rail.js    # Job stats + live running jobs widget
    └── log-viewer.js   # Dual-mode log viewer (drawer + panel)
```

---

## Component Registration Pattern

Vue components are loaded as plain `<script>` tags. Each component file wraps itself in an
IIFE and exports a factory function on `window`:

```js
// web/components/my-widget.js
(function (global) {
    const MyWidget = {
        name: 'MyWidget',
        props: { /* ... */ },
        emits: ['update:value'],
        setup(props, { emit }) { /* ... */ },
        template: `<div>...</div>`,
    };

    global.registerMyWidgetComponent = function (app) {
        app.component('my-widget', MyWidget);
    };
})(window);
```

The page script calls the factory **before** mounting:

```js
const app = Vue.createApp({ /* ... */ });
window.registerMyWidgetComponent(app);
app.mount('#app');
```

### Naming conventions

| Layer | Convention | Example |
|-------|-----------|---------|
| File | `kebab-case.js` | `my-widget.js` |
| Global factory | `registerXxxComponent` | `registerMyWidgetComponent` |
| Vue component name | PascalCase | `MyWidget` |
| HTML element tag | kebab-case | `<my-widget>` |

---

## Component Inventory

### `site-nav`

**File:** `web/components/site-nav.js`  
**Pages:** index.html, jobs.html, settings.html, login.html

Fixed top navigation bar. Renders the wordmark, page links with active-state highlighting,
a default slot for page-specific toolbar actions, an optional SSE live indicator, the theme
toggle button, and the logout form.

| Prop | Type | Default | Description |
|------|------|---------|-------------|
| `page` | String | `''` | Active link: `'index'` \| `'jobs'` \| `'settings'` |
| `right-offset` | String | `'0px'` | CSS right offset to shift nav flush with a collapsible sidebar |
| `sse-connected` | Boolean/null | `null` | `null` hides the live indicator; `true`/`false` shows connected/disconnected |

**Emits:** none — uses the default slot for page-specific toolbar content.

**Dark mode ownership:** `site-nav` is the **sole owner** of dark mode state across the
entire app. See [Theme System](#theme-system) below.

---

### `app-sidebar`

**File:** `web/components/app-sidebar.js`  
**Pages:** index.html only

Fixed right-hand console sidebar showing statistics counters, a tabbed activity/feed stream
log, and a logs-drawer toggle. Collapses to a 64 px icon rail.

| Prop | Type | Default | Description |
|------|------|---------|-------------|
| `collapsed` | Boolean | required | Collapsed (64 px) vs expanded (320 px) state |
| `stats` | Object | required | `{ pending, seen, staged, approved, rejected, queued }` |
| `logs-open` | Boolean | required | Whether the log drawer is currently open |
| `tab` | String | `'activity'` | Active tab: `'activity'` \| `'feed'` |
| `activities` | Array | `[]` | Activity log entries `{ id, action, torrent_title, match_reason, action_at }` |
| `feed-stream` | Array | `[]` | Feed items `{ id, title, size, status, match_reason }` |
| `format-size-fn` | Function | required | Formats a byte count into a human-readable size string |

**Emits:**

| Event | Payload | Description |
|-------|---------|-------------|
| `toggle-collapse` | — | User clicked the collapse/expand handle |
| `update:tab` | tab name | Tab selection changed |
| `toggle-logs` | — | Logs-drawer toggle clicked |

---

### `torrent-card`

**File:** `web/components/torrent-card.js`  
**Pages:** index.html only

Single torrent row card showing a status badge, title, size, match reason, AI score where
present, and context-sensitive action buttons.

| Prop | Type | Default | Description |
|------|------|---------|-------------|
| `torrent` | Object | required | `{ status, title, size, match_reason, ai_scored, ai_score, ai_reason, match_confidence, match_confidence_reason }` |
| `selected` | Boolean | `false` | Card is in the multi-select selection set |
| `multi-select-active` | Boolean | `false` | Multi-select mode on — hides single-card action buttons |
| `active-tab` | String | `'pending'` | Current tab context: `'pending'` \| `'accepted'` |
| `operating` | Boolean | `false` | Async operation in progress — disables buttons and shows spinner |

**Emits:**

| Event | Payload |
|-------|---------|
| `toggle-select` | — |
| `approve` | — |
| `reject` | — |
| `queue` | — |
| `rematch` | — |
| `rescore` | — |

---

### `ops-banner`

**File:** `web/components/ops-banner.js`  
**Pages:** index.html, jobs.html

Compact banner showing one or more in-progress async operations with a progress bar and
cancel button. Expands to a scrollable list when multiple jobs run simultaneously.

| Prop | Type | Default | Description |
|------|------|---------|-------------|
| `running-jobs` | Array | required | `[{ id, type, progress }]` |
| `cancel-job-fn` | Function | `null` | Called with a job `id` to cancel it |

**Emits:** none — cancellation is handled via the `cancel-job-fn` callback prop.

---

### `jobs-rail`

**File:** `web/components/jobs-rail.js`  
**Pages:** index.html, jobs.html

Dashboard widget showing job counters (running / failed / cancelled), batch progress, a
live list of running jobs with cancel buttons, and the most recent completed/failed job.

| Prop | Type | Default | Description |
|------|------|---------|-------------|
| `running-count` | Number | required | Currently running job count |
| `failed-count` | Number | required | Failed job count |
| `cancelled-count` | Number | required | Cancelled job count |
| `cancel-job-fn` | Function | `null` | Called with a job `id` to cancel it |
| `batch-stats` | Object | required | `{ total, completed, running }` |
| `rail-running-jobs` | Array | required | Running job objects `{ id, type, started_at, progress }` |
| `latest-terminal-job` | Object | `null` | Most recent completed/failed/cancelled job |
| `format-relative-fn` | Function | required | Formats a timestamp to a relative string (e.g. `"2m ago"`) |
| `job-summary-line-fn` | Function | required | Generates a one-line summary string for a job object |
| `subtitle` | String | `''` | Subtitle text below "async jobs" heading |
| `link-href` | String | `''` | Optional href for an "open jobs →" link button |
| `link-text` | String | `''` | Optional label for that link button |
| `right-label` | String | `''` | Right-side label shown when no link is configured |
| `class-name` | String | `''` | Additional CSS classes for the container element |

**Emits:** none — cancellation is handled via the `cancel-job-fn` callback prop.

---

### `log-viewer`

**File:** `web/components/log-viewer.js`  
**Pages:** index.html (drawer mode), jobs.html (panel mode), settings.html (panel mode)

Bidirectional log viewer. Manages its own SSE connection to `/api/logs`, supports filtering
by level and free text, toggleable sort order, auto-scroll, and drag-to-resize in drawer
mode.

| Prop | Type | Default | Description |
|------|------|---------|-------------|
| `variant` | String | `'panel'` | `'drawer'` — fixed bottom floating panel; `'panel'` — inline collapsible section |
| `open` | Boolean | `false` | Drawer variant: externally controlled open state |
| `drawer-height` | String | `'60vh'` | Drawer variant: initial height CSS value |
| `drawer-right` | String | `'320px'` | Drawer variant: CSS right offset (keep in sync with sidebar width) |
| `title` | String | `'// application logs'` | Label shown in the viewer toolbar |

**Emits:**

| Event | Payload | Description |
|-------|---------|-------------|
| `close` | — | Drawer closed by the user |
| `height-change` | height string | User dragged the resize handle |

---

### `auth-guard` *(utility module)*

**File:** `web/components/auth-guard.js`  
**Pages:** all authenticated pages (index, jobs, settings)

Not a Vue component. Patches `window.fetch` to intercept `401 Unauthorized` responses and
redirect the browser to `/login`. Also exposes `window.__authProbe()` — used by SSE error
handlers that cannot inspect HTTP status codes directly — to verify session validity before
treating a connection failure as an auth error.

---

## Design Token System

Design tokens live in `:root` (light) and `.dark` (dark) blocks in `web/style.css`. All
values are **bare RGB triplets**, which enables Tailwind opacity modifiers without extra
syntax:

```css
/* correct — opacity modifier works */
background-color: rgb(var(--c-surface) / 0.6);
```

### Surfaces

| Token | Light (RGB) | Light swatch | Dark (RGB) | Dark swatch |
|-------|------------|--------------|-----------|-------------|
| `--c-surface` | `253 249 243` | warm ivory page bg | `3 7 18` | gray-950 |
| `--c-card` | `255 253 247` | cream section/panel bg | `17 24 39` | gray-900 |
| `--c-raised` | `255 255 255` | white inner tiles, dropdowns | `31 41 55` | gray-800 |
| `--c-deep` | `244 240 235` | ecru sunken/input bg | `3 7 18` | gray-950 (same as surface) |

### Borders

| Token | Light | Dark |
|-------|-------|------|
| `--c-border-soft` | `229 224 215` | `31 41 55` |
| `--c-border-base` | `207 202 192` | `55 65 81` |

### Text

| Token | Light | Role |
|-------|-------|------|
| `--c-fg` | `28 25 23` | Primary body text (stone-950) |
| `--c-fg-soft` | `87 83 78` | Secondary text (stone-600) |
| `--c-fg-dim` | `120 113 108` | Tertiary / placeholder (stone-500) |
| `--c-fg-muted` | `168 162 158` | Disabled / very subdued (stone-400) |
| `--c-fg-faint` | `214 211 208` | Decorative hairlines (stone-300) |

Dark overrides shift the whole ramp to blue-gray (`gray-100` → `gray-700`).

### Curator accent (green)

| Token | Light | Dark |
|-------|-------|------|
| `--c-accent` | `22 163 74` | `0 255 65` (#00ff41) |
| `--c-accent-bg` | `34 197 94` | `0 255 65` |
| `--c-accent-bg-hover` | `22 163 74` | `0 221 56` (#00dd38) |
| `--c-accent-surface` | `240 253 244` | `0 61 16` (#003d10) |
| `--c-accent-border` | `34 197 94` | `0 255 65` |

---

## Semantic Utility Classes

`web/style.css` provides utility classes that map to design tokens. **Use these instead of
raw Tailwind color classes** so both themes work automatically without any `dark:` variants.

### Surfaces

| Class | CSS property |
|-------|-------------|
| `.bg-surface` | `background-color: rgb(var(--c-surface))` |
| `.bg-card` | `background-color: rgb(var(--c-card))` |
| `.bg-raised` | `background-color: rgb(var(--c-raised))` |
| `.bg-deep` | `background-color: rgb(var(--c-deep))` |
| `.bg-accent-surface` | `background-color: rgb(var(--c-accent-surface))` |

Opacity variants available: `.bg-raised/30`, `.bg-raised/40`, `.bg-raised/50`,
`.bg-raised/60`, `.bg-raised/80`, `.bg-card/60`, `.bg-deep/60`.

### Text

| Class | CSS property |
|-------|-------------|
| `.fg-base` | `color: rgb(var(--c-fg))` |
| `.fg-soft` | `color: rgb(var(--c-fg-soft))` |
| `.fg-dim` | `color: rgb(var(--c-fg-dim))` |
| `.fg-muted` | `color: rgb(var(--c-fg-muted))` |
| `.fg-faint` | `color: rgb(var(--c-fg-faint))` |
| `.fg-accent` | `color: rgb(var(--c-accent))` |
| `.text-accent` | `color: rgb(var(--c-accent))` — Tailwind-compatible alias |

### Borders

| Class | CSS property |
|-------|-------------|
| `.border-subtle` | `border-color: rgb(var(--c-border-soft))` |
| `.border-base` | `border-color: rgb(var(--c-border-base))` |
| `.border-accent` | `border-color: rgb(var(--c-accent-border))` |
| `.border-l-accent` | `border-left-color: rgb(var(--c-accent-border))` |
| `.border-t-accent` | `border-top-color: rgb(var(--c-accent-border))` |

Hover variant: `.hover:border-accent`.

### Badges

Each badge class sets `background-color`, `color`, and `border-color` for both themes.
Apply Tailwind's `border` class separately to set `border-width`.

| Class | Semantic use |
|-------|-------------|
| `.badge-blue` | Pending torrent, feed-check job type |
| `.badge-accent` | Accepted/approved torrent, running state |
| `.badge-red` | Rejected torrent, failed job, error states |
| `.badge-amber` | Low-confidence match, cancelled job, warn states |
| `.badge-emerald` | AI score display, completed job badge |
| `.badge-purple` | Rescore job type |
| `.badge-indigo` | Genre tags (suggestions), rematch job type |
| `.badge-green` | Available / success states (Settings) |

### Primary action buttons

| Class | Purpose |
|-------|---------|
| `.bg-accent` | Primary action button fill |
| `.hover:bg-accent` | Hover state |

---

## Theme System

### How it works end-to-end

1. `<html class="dark">` activates the `.dark` token block and Tailwind's dark-mode
   variants across every page.

2. `site-nav.js` is the **single owner** of this state:
   - On `onMounted`, reads `localStorage['rss-curator-dark-mode']`. If a value is stored,
     applies it. If absent, falls back to the OS `prefers-color-scheme` query.
   - When the user clicks the theme toggle, saves the new value to `localStorage` and
     re-applies the class.
   - Installs a `matchMedia` change listener for `prefers-color-scheme` — fires only when
     no manual override is stored.

3. Because `site-nav` mounts asynchronously, a fast page load without a stored preference
   can briefly show the wrong theme (FOUC). `jobs.html` and `settings.html` include a
   synchronous inline IIFE in `<head>` that applies `.dark` before Vue mounts:

```html
<script>
    (function () {
        var s = localStorage.getItem('rss-curator-dark-mode');
        if (s === 'true' || (!s && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
            document.documentElement.classList.add('dark');
        }
    })();
</script>
```

`index.html` does not need this script because `app.js` mounts fast enough.

**Do not add `darkMode` props or toggle logic to new components** — that responsibility
belongs to `site-nav` alone.

### localStorage key

`rss-curator-dark-mode` — stored as the string `'true'` or `'false'`, or absent (follows OS
preference when absent).

---

## Page → Component Wiring

| Component | index.html | jobs.html | settings.html | login.html |
|-----------|:---:|:---:|:---:|:---:|
| `<site-nav>` | ✓ | ✓ | ✓ | ✓ |
| `<app-sidebar>` | ✓ | — | — | — |
| `<torrent-card>` | ✓ | — | — | — |
| `<ops-banner>` | ✓ | ✓ | — | — |
| `<jobs-rail>` | ✓ | ✓ | — | — |
| `<log-viewer>` (drawer) | ✓ | — | — | — |
| `<log-viewer>` (panel) | — | ✓ | ✓ | — |
| `auth-guard` (utility) | ✓ | ✓ | ✓ | — |

---

## Adding a New Component

1. **Create** `web/components/my-widget.js` using the IIFE/factory pattern:

```js
(function (global) {
    const MyWidget = {
        name: 'MyWidget',
        props: {
            value: { type: String, default: '' },
        },
        emits: ['update:value'],
        setup(props, { emit }) {
            // composition logic here
        },
        template: `
            <div class="bg-card border border-base rounded-lg p-4">
                <!-- use semantic utility classes, not raw Tailwind color classes -->
            </div>
        `,
    };

    global.registerMyWidgetComponent = function (app) {
        app.component('my-widget', MyWidget);
    };
})(window);
```

2. **Load the script** in every HTML page that uses the component — add a `<script>` tag
   before `app.js` / `settings.js`:

```html
<script src="components/my-widget.js"></script>
```

3. **Register with the app** before `app.mount()`:

```js
window.registerMyWidgetComponent(app);
```

4. **Use the tag** in the Vue template:

```html
<my-widget :value="someRef" @update:value="val => someRef = val" />
```

5. **Styling rules:**
   - Use semantic utility classes (`.bg-card`, `.fg-base`, `.border-subtle`, `.badge-*`)
     instead of raw Tailwind color classes.
   - Dark mode is automatic — token classes handle it; no `dark:` Tailwind variants needed.
   - Do **not** add a `darkMode` prop or toggle logic — that belongs to `site-nav`.

---

## Planned Future Extractions

These UI sections are currently inline and are candidates for extraction in a future session:

| Target | Current location | Notes |
|--------|-----------------|-------|
| Modal dialogs (approve / reject / confirm) | Inline in `app.js` template | Several modals share a common overlay/backdrop pattern |
| Login widget | Inline in `login.html` | Extraction would enable reuse if a re-auth modal is ever added |
