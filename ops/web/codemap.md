# ops/web/

## Responsibility
Web frontend assets root. Contains static files (CSS, JS) and Pongo2 HTML templates for the HTMX-based UI.

## Design
- `static/css/` — Single CSS file (`styles.css`) with cyberpunk terminal palette
- `static/js/` — Minimal vanilla JS (`app.js`) for modals, console, filters
- `templates/layouts/` — Base layout (`base.html`) with common head/nav/footer
- `templates/pages/` — Full page templates (watchlists, libraries, profiles, schedules, artists, jobs, dashboard)
- `templates/partials/` — HTMX partial fragments for dynamic DOM updates

## Integration
- **Consumed by**: `cmd/server` (Fiber static middleware + Pongo2 engine)
- **Pattern**: Server-rendered HTMX — no SPA framework, no build step
