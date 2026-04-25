# ops/web/templates/pages/

## Responsibility
Full page templates extending the base layout.

## Design
| File | Purpose |
|------|----------|
| `index.html` | Dashboard: login overlay, stats region, watchlists region, console region |
| `watchlists.html` | Watchlist management page |
| `libraries.html` | Music library configuration |
| `profiles.html` | Download/source profiles |
| `schedules.html` | Scheduled sync jobs |
| `artists.html` | Artist tracking |
| `jobs.html` | Job history and monitoring |

## Flow
- Each page extends `layouts/base.html`
- Sets `{% block title %}` and `{% block content %}`
- Content regions use `hx-get` to load partials on load/intervals

## Integration
- **Backend**: Route handlers render these templates with context
- **HTMX**: Partial endpoints (e.g., `/partials/watchlists`) supply dynamic content