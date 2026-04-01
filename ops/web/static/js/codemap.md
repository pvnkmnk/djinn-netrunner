# ops/web/static/js/

## Responsibility
Minimal client-side JavaScript for modal management and console controls.

## Design
| File | Purpose |
|------|---------|
| `app.js` | Modal handling, log filtering, copy to clipboard, auto-scroll, WebSocket message handling |

## Flow
- Deferred script load in base.html
- Listens for HTMX events: `htmx:afterOnLoad`, `htmx:wsMessage`
- Functions: `openModal()`, `closeModal()`, filter/log management

## Integration
- **HTMX**: Handles modal triggers via response headers
- **WebSocket**: Appends live log messages to console
- **Console controls**: Filter buttons, copy, clear, resume live scroll