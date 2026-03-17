# Phase 2: UI Operational Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create operational UI with HTMX templates, single CSS file, and minimal JS for console controls.

**Architecture:** 
- Server-rendered HTML with Fiber templates
- HTMX for partial page updates (stats, watchlists)
- WebSocket for live console log streaming
- Single CSS file (per docs constraint)
- Minimal JS for console controls (auto-scroll, filters, copy)

**Tech Stack:** Go Fiber, HTMX, WebSockets, Vanilla CSS/JS

---

## Task 1: Create Directory Structure

**Files:**
- Create: `ops/web/templates/`
- Create: `ops/web/static/`

**Step 1: Create directories**

```bash
mkdir -p ops/web/templates/layouts
mkdir -p ops/web/templates/partials
mkdir -p ops/web/static
```

**Step 2: Commit**

```bash
git add ops/web/
git commit -m "chore: create UI directory structure"
```

---

## Task 2: Create Base Layout Template

**Files:**
- Create: `ops/web/templates/layouts/base.html`

**Step 1: Create base layout**

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{% block title %}NETRUNNER{% endblock %}</title>
    <link rel="stylesheet" href="/static/css/styles.css">
    <script src="https://unpkg.com/htmx.org@1.9.10"></script>
    <script src="https://unpkg.com/htmx.org@1.9.10/dist/ext/ws"></script>
    {% block head %}{% endblock %}
</head>
<body>
    <header class="app-header">
        <h1>NETRUNNER</h1>
        <nav>
            <a href="/">Dashboard</a>
            <a href="/watchlists">Watchlists</a>
            <a href="/artists">Artists</a>
            <a href="/schedules">Schedules</a>
        </nav>
    </header>
    
    <main class="app-main">
        {% block content %}{% endblock %}
    </main>
    
    <footer class="app-footer">
        <p>NETRUNNER - Music Acquisition Pipeline</p>
    </footer>
    
    <script src="/static/js/app.js"></script>
    {% block scripts %}{% endblock %}
</body>
</html>
```

**Step 2: Commit**

```bash
git add ops/web/templates/layouts/base.html
git commit -m "feat: add base layout template"
```

---

## Task 3: Create Dashboard Index Template

**Files:**
- Create: `ops/web/templates/index.html`

**Step 1: Create index template**

```html
{% extends "layouts/base.html" %}

{% block title %}Dashboard - NETRUNNER{% endblock %}

{% block content %}
<div class="dashboard">
    <section class="stats-region" hx-get="/partials/stats" hx-trigger="every 30s">
        <!-- Stats loaded via HTMX -->
        <div class="stats-loading">Loading stats...</div>
    </section>
    
    <section class="watchlists-region" hx-get="/partials/watchlists" hx-trigger="every 60s">
        <!-- Watchlists loaded via HTMX -->
        <div class="watchlists-loading">Loading watchlists...</div>
    </section>
    
    <section class="console-region">
        <div class="console-controls">
            <button class="btn" id="btn-attach" hx-post="/console/attach">Attach</button>
            <button class="btn" id="btn-detach">Detach</button>
            <span class="filter-group">
                <button class="filter-btn active" data-filter="all">ALL</button>
                <button class="filter-btn" data-filter="OK">OK</button>
                <button class="filter-btn" data-filter="INFO">INFO</button>
                <button class="filter-btn" data-filter="ERR">ERR</button>
            </span>
            <button class="btn" id="btn-copy">Copy Last 200</button>
            <button class="btn" id="btn-clear">Clear</button>
            <button class="btn" id="btn-resume">Resume Live</button>
        </div>
        
        <div id="console-socket" class="console-socket">
            <!-- WebSocket connection swaps here -->
            <div class="console-placeholder">Select a job to attach</div>
        </div>
        
        <div id="console-logs" class="console-logs" hx-ext="ws" ws-connect="/ws/jobs">
            <!-- Log lines rendered here -->
        </div>
    </section>
</div>
{% endblock %}
```

**Step 2: Commit**

```bash
git add ops/web/templates/index.html
git commit -m "feat: add dashboard index template"
```

---

## Task 4: Create CSS Styles

**Files:**
- Create: `ops/web/static/css/styles.css`

**Step 1: Create CSS**

```css
/* NETRUNNER - Single CSS File */
/* Following docs constraint: minimal, single file */

:root {
    --bg-primary: #0d1117;
    --bg-secondary: #161b22;
    --bg-tertiary: #21262d;
    --text-primary: #c9d1d9;
    --text-secondary: #8b949e;
    --accent-primary: #58a6ff;
    --accent-success: #3fb950;
    --accent-warning: #d29922;
    --accent-error: #f85149;
    --border-color: #30363d;
    --font-mono: 'SF Mono', 'Fira Code', 'Consolas', monospace;
    --font-sans: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
}

* { box-sizing: border-box; margin: 0; padding: 0; }

body {
    font-family: var(--font-sans);
    background: var(--bg-primary);
    color: var(--text-primary);
    line-height: 1.5;
}

.app-header {
    background: var(--bg-secondary);
    border-bottom: 1px solid var(--border-color);
    padding: 1rem;
    display: flex;
    justify-content: space-between;
    align-items: center;
}

.app-header h1 { font-size: 1.25rem; font-weight: 600; }

.app-header nav a {
    color: var(--text-secondary);
    text-decoration: none;
    margin-left: 1.5rem;
    transition: color 0.15s;
}

.app-header nav a:hover, .app-header nav a.active {
    color: var(--accent-primary);
}

.app-main { padding: 1.5rem; max-width: 1400px; margin: 0 auto; }

.app-footer {
    text-align: center;
    padding: 1rem;
    color: var(--text-secondary);
    font-size: 0.875rem;
    border-top: 1px solid var(--border-color);
    margin-top: 2rem;
}

/* Stats */
.stats-region {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 1rem;
    margin-bottom: 2rem;
}

.stat-card {
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    padding: 1rem;
}

.stat-card .stat-value { font-size: 2rem; font-weight: 700; }
.stat-card .stat-label { color: var(--text-secondary); font-size: 0.875rem; }
.stat-card.success .stat-value { color: var(--accent-success); }
.stat-card.error .stat-value { color: var(--accent-error); }

/* Watchlists */
.watchlists-region { margin-bottom: 2rem; }

.watchlist-card {
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    padding: 1rem;
    margin-bottom: 0.75rem;
    display: flex;
    justify-content: space-between;
    align-items: center;
}

.watchlist-card .name { font-weight: 600; }
.watchlist-card .source { color: var(--text-secondary); font-size: 0.875rem; }
.watchlist-card .sync-status { font-size: 0.875rem; }

/* Console */
.console-region { background: var(--bg-secondary); border: 1px solid var(--border-color); border-radius: 6px; overflow: hidden; }

.console-controls {
    background: var(--bg-tertiary);
    padding: 0.75rem;
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
    border-bottom: 1px solid var(--border-color);
}

.btn {
    background: var(--bg-primary);
    border: 1px solid var(--border-color);
    color: var(--text-primary);
    padding: 0.375rem 0.75rem;
    border-radius: 4px;
    cursor: pointer;
    font-size: 0.875rem;
    transition: all 0.15s;
}

.btn:hover { border-color: var(--accent-primary); }
.btn:active { transform: scale(0.98); }

.filter-group { display: flex; gap: 0.25rem; margin-left: auto; }

.filter-btn {
    background: transparent;
    border: none;
    color: var(--text-secondary);
    padding: 0.25rem 0.5rem;
    cursor: pointer;
    font-size: 0.75rem;
}

.filter-btn.active { color: var(--accent-primary); font-weight: 600; }

.console-logs {
    height: 400px;
    overflow-y: auto;
    font-family: var(--font-mono);
    font-size: 0.8125rem;
    padding: 0.75rem;
    background: var(--bg-primary);
}

.log-line {
    padding: 0.125rem 0;
    border-bottom: 1px solid var(--bg-tertiary);
}

.log-line .log-ts { color: var(--text-secondary); margin-right: 0.5rem; }
.log-line .log-level { margin-right: 0.5rem; }
.log-line.log-ok .log-level { color: var(--accent-success); }
.log-line.log-info .log-level { color: var(--accent-primary); }
.log-line.log-warn .log-level { color: var(--accent-warning); }
.log-line.log-err .log-level { color: var(--accent-error); }

.console-placeholder { color: var(--text-secondary); text-align: center; padding: 2rem; }

/* Forms */
.form-group { margin-bottom: 1rem; }
.form-group label { display: block; margin-bottom: 0.375rem; font-size: 0.875rem; }
.form-group input, .form-group select {
    width: 100%;
    padding: 0.5rem;
    background: var(--bg-primary);
    border: 1px solid var(--border-color);
    border-radius: 4px;
    color: var(--text-primary);
}
.form-group input:focus { outline: none; border-color: var(--accent-primary); }

/* Tables */
.table { width: 100%; border-collapse: collapse; }
.table th, .table td { padding: 0.75rem; text-align: left; border-bottom: 1px solid var(--border-color); }
.table th { color: var(--text-secondary); font-weight: 600; font-size: 0.875rem; }
.table tr:hover { background: var(--bg-tertiary); }

/* Utility */
.text-success { color: var(--accent-success); }
.text-warning { color: var(--accent-warning); }
.text-error { color: var(--accent-error); }
.mt-1 { margin-top: 0.5rem; }
.mt-2 { margin-top: 1rem; }
.mb-1 { margin-bottom: 0.5rem; }
.mb-2 { margin-bottom: 1rem; }
```

**Step 2: Commit**

```bash
git add ops/web/static/css/styles.css
git commit -m "feat: add UI stylesheet"
```

---

## Task 5: Create Minimal Console JS

**Files:**
- Create: `ops/web/static/js/app.js`

**Step 1: Create JS**

```javascript
// NETRUNNER - Minimal Console JS
// Following docs: minimal JS only for console controls

document.addEventListener('DOMContentLoaded', function() {
    const consoleLogs = document.getElementById('console-logs');
    const filterBtns = document.querySelectorAll('.filter-btn');
    let autoScroll = true;
    let filter = 'all';
    
    // Filter buttons
    filterBtns.forEach(btn => {
        btn.addEventListener('click', function() {
            filterBtns.forEach(b => b.classList.remove('active'));
            this.classList.add('active');
            filter = this.dataset.filter;
            applyFilter();
        });
    });
    
    function applyFilter() {
        const lines = consoleLogs.querySelectorAll('.log-line');
        lines.forEach(line => {
            if (filter === 'all') {
                line.style.display = '';
            } else {
                const level = line.querySelector('.log-level');
                if (level && level.textContent.includes(filter)) {
                    line.style.display = '';
                } else {
                    line.style.display = 'none';
                }
            }
        });
    }
    
    // Auto-scroll
    if (consoleLogs) {
        consoleLogs.addEventListener('scroll', function() {
            const atBottom = this.scrollHeight - this.scrollTop <= this.clientHeight + 50;
            autoScroll = atBottom;
        });
    }
    
    // Resume Live button
    const resumeBtn = document.getElementById('btn-resume');
    if (resumeBtn) {
        resumeBtn.addEventListener('click', function() {
            autoScroll = true;
            if (consoleLogs) {
                consoleLogs.scrollTop = consoleLogs.scrollHeight;
            }
        });
    }
    
    // Copy Last 200 button
    const copyBtn = document.getElementById('btn-copy');
    if (copyBtn) {
        copyBtn.addEventListener('click', function() {
            const lines = Array.from(consoleLogs.querySelectorAll('.log-line'))
                .slice(-200)
                .map(line => line.textContent)
                .join('\n');
            navigator.clipboard.writeText(lines).then(() => {
                copyBtn.textContent = 'Copied!';
                setTimeout(() => copyBtn.textContent = 'Copy Last 200', 2000);
            });
        });
    }
    
    // Clear button
    const clearBtn = document.getElementById('btn-clear');
    if (clearBtn) {
        clearBtn.addEventListener('click', function() {
            consoleLogs.innerHTML = '';
        });
    }
    
    // WebSocket message handler (if using htmx ws)
    document.body.addEventListener('htmx:wsMessage', function(evt) {
        const msg = evt.detail.message;
        if (msg && consoleLogs) {
            const div = document.createElement('div');
            div.innerHTML = msg;
            consoleLogs.appendChild(div);
            if (autoScroll) {
                consoleLogs.scrollTop = consoleLogs.scrollHeight;
            }
        }
    });
});
```

**Step 2: Commit**

```bash
git add ops/web/static/js/app.js
git commit -m "feat: add minimal console JS"
```

---

## Task 6: Create HTMX Partial Endpoints

**Files:**
- Modify: `backend/cmd/server/main.go`

**Step 1: Add partial routes**

Add routes for HTMX partials in main.go:

```go
// HTMX partials
app.Get("/partials/stats", dash.RenderStatsPartial)
app.Get("/partials/watchlists", watchlistHandler.RenderWatchlistsPartial)
```

**Step 2: Add partial render methods**

Add to `dashboard.go`:
```go
func (h *DashboardHandler) RenderStatsPartial(c *fiber.Ctx) error {
    // Return just the stats HTML
    return c.Render("partials/stats", fiber.Map{})
}
```

**Step 3: Commit**

```bash
git add backend/cmd/server/main.go backend/internal/api/dashboard.go
git commit -m "feat: add HTMX partial endpoints"
```

---

## Task 7: Run Build and Verify

**Step 1: Build and verify**

```bash
cd backend && go build ./... && echo "Build successful"
```

**Step 2: Commit**

```bash
git commit -m "test: verify build after adding UI"
```

---

## Plan Complete

**Total Tasks:** 7
**Estimated Time:** 30-45 minutes

---

## Execution Choice

**Plan complete and saved to `docs/plans/2026-03-17-phase2-ui-operational.md`.**

Two execution options:

1. **Subagent-Driven (this session)** - I dispatch fresh fixer subagent per task, review between tasks, fast iteration

2. **Parallel Session (separate)** - Open new session with executing-plans, batch execution with checkpoints

Which approach?
