# backend/internal/api/templates/

## Responsibility
Pongo2-based template engine for Fiber. Renders Jinja2-compatible templates for both full pages and HTMX partials. Templates define the visual presentation layer using server-side rendering with data binding from handlers.

## Design

### Core Components

**Pongo2Engine** - Implements Fiber's ViewEngine interface:
```go
type Pongo2Engine struct {
    directory string           // Template root directory
    extension string           // File extension (e.g., ".html")
    pool      sync.Pool       // Template set reuse for performance
}
```

**Key Behaviors:**
- Uses `sync.Pool` to cache pongo2.TemplateSet instances - avoids repeated file system lookups
- Normalizes Windows paths to forward slashes for pongo2 compatibility
- Tries template name as-is, then with extension appended
- Preloads all templates from directory tree on startup via `LoadFromDir()`
- Supports Jinja2-style template inheritance (`{% extends %}`) and blocks (`{% block %}`)

**Registered Filters:**
- `upper` - String uppercase conversion (other filters pass pre-formatted data)

### Template Structure
- **Layouts** - Base templates with `{% extends %}` for page structure
- **Pages** - Full-page templates in `pages/` directory (watchlists.html, libraries.html, etc.)
- **Partials** - HTMX-replaceable fragments in `partials/` directory (watchlists, jobs, stats, forms)

### Data Binding
Templates receive `fiber.Map` from handlers with keys like:
- `jobs`, `watchlists`, `libraries`, `profiles`, `schedules`, `artists` - collections
- `stats` - JobStats/StatsData structs for dashboard
- `User`, `authUserID` - authentication context
- Form templates receive specific fields (ID, Name, SourceType, etc.)

## Flow

### Template Loading (Startup)
1. Server calls `engine.Load()` which invokes `LoadFromDir()`
2. Glob finds all `*.html` files under templates directory
3. Each template preloaded into pongo2.TemplateSet
4. Sets cached in sync.Pool for reuse

### Full Page Render Flow
1. Handler calls `c.Render("pages/watchlists", fiber.Map{...})`
2. Pongo2Engine.Render acquires TemplateSet from pool
3. Template file loaded, parsed with extends/imports resolved
4. Context (fiber.Map) converted to pongo2.Context
5. Template executed, output written to response writer
6. TemplateSet returned to pool

### Partial Render Flow
1. HTMX request detected via header
2. Handler calls `c.Render("partials/watchlists", fiber.Map{...})`
3. Same render path, but template is just the fragment
4. HTMX receives HTML and swaps into page

### Error Handling
- Template not found: returns wrapped error with template name
- Render error: returns wrapped error with template name and underlying cause

## Integration

### Dependencies
- **flosch/pongo2/v6** - Jinja2-compatible template engine
- **gofiber/fiber/v2** - ViewEngine interface implementation

### Providers (Templates Supplied By)
Templates stored in `backend/templates/` directory with subdirectories:
- `pages/` - Full page templates
- `partials/` - HTMX partials
- Layout/base templates at root

### Consumers
- **api handlers** - Call `c.Render()` with template names and fiber.Map data
- **HTMX bootstrap** - Partial templates swapped into live pages
- **WebSocket** - Job logs formatted as HTML and sent via WebSocket (not this engine)