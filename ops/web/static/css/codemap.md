# ops/web/static/css/

## Responsibility
Single-file CSS stylesheet for the entire web UI.

## Design
- `styles.css` — terminal/cyberpunk aesthetic with sharp edges, minimal animation
- CSS custom properties for theming (dark palette by default)
- No CSS framework, no preprocessors, no build step
- Responsive layout via CSS Grid/Flexbox

## Integration
- **Consumed by**: `layouts/base.html` (linked in `<head>`)
- **Invariant**: Single CSS file — do not split per AGENTS.md
