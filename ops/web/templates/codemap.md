# ops/web/templates/

## Responsibility
HTML templates using Go template inheritance for server-rendered pages.

## Design
| Directory | Purpose |
|-----------|----------|
| `layouts/` | Base template (base.html) with header, nav, footer |
| `pages/` | Full pages: watchlists, schedules, profiles, libraries, jobs, artists |
| `partials/` | HTMX-swappable fragments: stats, forms, cards, lists |
| `index.html` | Dashboard page (login + stats + console) |

## Flow
1. Pages extend `layouts/base.html`
2. Base defines `{% block content %}` slot
3. Pages embed partials via HTMX `hx-get="/partials/..."`
4. Partials refresh on intervals (30s stats, 60s watchlists)

## Integration
- **Backend**: Fiber renders templates with context data
- **HTMX**: Partial endpoints return just the fragment
- **WebSocket**: `/ws/jobs` streams log lines to console div