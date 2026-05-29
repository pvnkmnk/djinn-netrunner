# ops/web/

## Responsibility
Root of web assets: static files and templates served by the Go backend.

## Design
| Directory | Contents |
|-----------|----------|
| `static/` | CSS, JS, images |
| `templates/` | HTML with Go template syntax (extends, block, partial) |

## Flow
1. Backend serves `/static/*` from `static/` directory
2. Templates use Go template inheritance (layouts → pages → partials)
3. HTMX triggers refresh partials via `/partials/*` endpoints
4. WebSocket at `/ws/jobs` streams job logs to console

## Integration
- **Backend**: Fiber app serves these files (STATIC_FILES_PATH, TEMPLATES_PATH env)
- **Frontend**: HTMX loads partials, WebSocket streams live logs