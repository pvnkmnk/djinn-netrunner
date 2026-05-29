# ops/web/templates/layouts/

## Responsibility
Base layout template inherited by all pages.

## Design
| File | Purpose |
|------|---------|
| `base.html` | Common structure: header (logo + nav), main slot, footer, CSS/JS includes |

## Flow
- All pages start with `{% extends "layouts/base.html" %}`
- Defines `{% block title %}` and `{% block content %}` slots
- Nav links: Dashboard, Watchlists, Libraries, Profiles, Schedules, Artists, Jobs
- Includes: htmx.org CDN, styles.css, app.js

## Integration
- **Backend**: Renders extended template with page-specific content
- **HTMX**: Uses `hx-get` on nav links (full page loads)