# ops/web/static/

## Responsibility
Static assets served by the web application.

## Design
| Directory | Contents |
|-----------|----------|
| `css/` | Stylesheets (styles.css) |
| `js/` | Client-side JavaScript (app.js) |

## Flow
- Served at `/static/css/*` and `/static/js/*`
- Referenced in base.html layout

## Integration
- **Base layout**: `<link rel="stylesheet" href="/static/css/styles.css">`
- **Base layout**: `<script src="/static/js/app.js" defer></script>`