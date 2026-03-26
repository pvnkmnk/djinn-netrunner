# ops/web/static/js/

## Responsibility
Minimal vanilla JavaScript for UI interactions that HTMX cannot handle.

## Design
- `app.js` — modal open/close, console log streaming, filter/search, keyboard shortcuts
- No framework, no build step, no npm dependencies
- Interacts with HTMX events via `htmx:afterSwap` listeners
- WebSocket client for real-time job log display

## Integration
- **Consumed by**: `layouts/base.html` (loaded via `<script>`)
- **Complements**: HTMX for dynamic content (HTMX handles server-driven DOM swaps, JS handles client-only interactions)
