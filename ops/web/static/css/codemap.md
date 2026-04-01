# ops/web/static/css/

## Responsibility
Stylesheets for the NETRUNNER UI.

## Design
| File | Purpose |
|------|---------|
| `styles.css` | Full cyberpunk theme: dark backgrounds, neon accents, terminal palette |

## Flow
- Loaded in `layouts/base.html` head
- CSS variables define theme (colors, fonts, spacing)
- Classes: `.stat-card`, `.watchlist-card`, `.job-card`, `.console-*`, etc.

## Integration
- **Used by**: All pages extending `layouts/base.html`
- **Theme**: Cyan/magenta neon accents on dark backgrounds