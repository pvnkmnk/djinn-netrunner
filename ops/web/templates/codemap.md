# ops/web/templates/

## Responsibility
Pongo2 (Jinja2-compatible) HTML templates for server-side rendering.

## Design
- `layouts/base.html` — master layout with `<head>`, nav, footer, CSS/JS includes
- `pages/` — full page templates: `watchlists.html`, `libraries.html`, `profiles.html`, `schedules.html`, `artists.html`, `jobs.html`, `dashboard.html`
- `partials/` — HTMX fragment templates (~16 files) for dynamic updates: `watchlist-card.html`, `artist-card.html`, `job-row.html`, `console-log.html`, etc.
- Template inheritance: pages `{% extends "layouts/base.html" %}` and fill blocks
- Partials are standalone fragments returned by HTMX-triggered endpoints

## Flow
1. Page request → Fiber → Pongo2 renders page template with context data
2. HTMX interaction → Fiber → Pongo2 renders partial → returned as HTML fragment
3. HTMX swaps partial into DOM

## Integration
- **Consumed by**: `internal/api/templates` (Pongo2Engine), `internal/api` (handler renders)
- **Pattern**: Server-rendered HTMX — templates are the UI, not JavaScript
