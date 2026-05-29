# ops/web/templates/partials/

## Responsibility
HTMX-swappable fragments for dynamic content updates.

## Design
| File | Purpose |
|------|----------|
| `stats.html` | Job counts: queued, running, succeeded (24h), failed (24h) |
| `watchlists.html` | List of watchlists with status |
| `watchlist-preview.html` | Single watchlist summary |
| `watchlist-form.html` | Add/edit watchlist modal |
| `libraries.html` | Library list |
| `library-form.html` | Add/edit library modal |
| `profiles.html` | Source profiles list |
| `profile-form.html` | Add/edit profile modal |
| `schedules.html` | Scheduled job list |
| `schedule-form.html` | Add/edit schedule modal |
| `schedule-card.html` | Single schedule card |
| `artists.html` | Tracked artists |
| `artist-card.html` | Single artist card |
| `artist-form.html` | Add/edit artist modal |
| `jobs.html` | Job list with filters |
| `jobs.html` | Job list |

## Flow
- Loaded via HTMX `hx-get="/partials/name"` on page load/trigger
- Refresh intervals: stats every 30s, watchlists every 60s
- Forms submit to API endpoints, then reload parent partial

## Integration
- **Backend**: `/partials/*` routes return just the fragment (no layout)
- **HTMX**: `hx-swap="outerHTML"` replaces region content
- **Modals**: Forms use `HX-Trigger: openModal` header to open modal after swap