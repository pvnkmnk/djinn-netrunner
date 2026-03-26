# backend/internal/api/templates/

## Responsibility
Pongo2 (Jinja2-compatible) template engine adapter for Fiber. Implements `fiber.ViewEngine` interface for server-side rendering.

## Design
- `Pongo2Engine` struct implementing `fiber.ViewEngine`
- `sync.Pool` for `TemplateSet` caching and concurrent rendering performance
- Preloads all templates on startup from `ops/web/templates/`
- Template directory structure: `layouts/`, `pages/`, `partials/`
- Supports Jinja2 inheritance (`{% extends "layouts/base.html" %}`) and blocks
- Custom template functions registered by `cmd/server`

## Flow
1. Engine created with template root path → scans and preloads all `.html` files
2. `Load()` called by Fiber → retrieves `TemplateSet` from pool → renders template with context
3. Returns rendered HTML string to Fiber for response

## Integration
- **Consumed by**: `cmd/server` (Fiber app initialization), `internal/api` (handler renders)
- **Consumes**: Template files from `ops/web/templates/`
- **Interface**: `fiber.ViewEngine` (`Load() (string, error)`)
