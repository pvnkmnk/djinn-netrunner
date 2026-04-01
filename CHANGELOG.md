# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [2.2.0] - 2026-04-01

### 🛡️ Security

- **BOLA Protection for Watchlists**: Added ownership verification to `GetPreview` and `GetForm` endpoints. Non-admin users can only access watchlists they own. Unauthorized access returns 403 Forbidden or "Watchlist not found" error snippets for HTMX partials.
- **BOLA Protection for Statistics**: All aggregate statistics and jobs listing endpoints now filter by `owner_user_id`. Non-admin users see only their own resource counts and data. Admins retain global visibility.
- **BOLA Protection for Libraries and Quality Profiles**: Ownership tracking enforced across library and quality profile endpoints.
- **Library Path Validation**: Added `validateLibraryPath()` to prevent path traversal attacks. Ensures library paths are absolute, exist, and are directories. All paths stored in canonical cleaned form via `filepath.Clean()`.
- **HTMX Partial Security**: HTMX form partials now incorporate ownership filters directly into queries, ensuring unauthorized access is treated as "Not Found" rather than leaking configuration details.

### ⚡ Performance

- **Eliminated Redundant Auth Lookups**: Removed duplicate database session lookups in `ArtistsHandler`, `StatsHandler`, `WatchlistPreviewHandler`, and `DashboardHandler`. All handlers now use `c.Locals("user")` populated by `AuthMiddleware`, saving 1+ database roundtrip per request.
- **Consolidated Stats Queries**: Replaced multiple sequential `COUNT` queries in `StatsHandler` with single SQL statements using subqueries and conditional aggregation (`COUNT(*) FILTER`). Reduced database roundtrips from up to 6 down to 1 for dashboard endpoints.
- **Optimized Watchlist Filtering**: Improved memory usage in watchlist filtering operations.

### ♿ Accessibility & UX

- **Contextual ARIA Labels**: Added descriptive `aria-label` attributes to all action buttons (Edit, Delete, Sync, Scan, Enrich) including the item name for screen reader context.
- **Specific Confirmation Messages**: Updated `hx-confirm` dialogs for destructive actions to include the specific item name being deleted, reducing accidental deletions.
- **HTMX Visual Feedback**: Added global `.htmx-request` CSS style that reduces opacity and changes cursor to 'wait' during background requests.
- **Form Accessibility**: Added `aria-label` to the "Enabled" toggle in watchlists and "Add" buttons across all management sections.
- **Missing Confirmation Added**: Added `hx-confirm` dialog to the "Delete" button in Schedules view.

### 🧪 Testing

- **BOLA Integration Tests**: Added comprehensive test suites (`watchlist_bola_test.go`, `stats_bola_test.go`, `stats_auth_test.go`, `stats_auth_repro_test.go`) verifying cross-user data isolation.
- **Library Path Validation Tests**: Added unit and integration tests for `validateLibraryPath()` covering relative paths, non-existent paths, file-vs-directory, and traversal scenarios.
- **Test Field Name Fixes**: Corrected JSON field name assertions in `libraries_test.go` to use capitalized field names matching the database model.
- **Auth Test Alignment**: Updated `stats_test.go` and `auth_test.go` to align with new authentication requirements.

### 📝 Documentation

- Updated `.jules/sentinel.md` with BOLA vulnerability learnings and prevention strategies.
- Updated `.jules/bolt.md` with performance optimization patterns.
- Updated `.jules/palette.md` with accessibility patterns and HTMX feedback conventions.

### 🐛 Bug Fixes

- Fixed UUID type mismatches in test suites.
- Fixed User struct field assertions in tests.
- Fixed misplaced `//go:build` comments.
- Removed unused imports (`net/http`, `ctx` in tests).

---

## [2.1.0] - 2026-03-26

### Added
- Phase 2: Pipeline Architecture + Quality System (DJI-21 through DJI-24)
- Phase 1: Security Hardening + Test Foundation (DJI-9 through DJI-20)
- Phase 0: Harden sprint fixes (DJI-5..DJI-8)
- Integration test harness
- Repository cartography with hierarchical codemaps

### Security
- Fixed BOLA in Libraries and Quality Profiles
- Fixed BOLA in monitored artists management
- Fixed BOLA in schedules management

### Performance
- Optimized library scanner performance
- Optimized SyncDiscography N+1 queries
- Consolidated dashboard stats queries
- Batch job item creation and consolidated progress queries

### UI/UX
- Enhanced artist monitoring accessibility and clarity
- Enhanced dashboard accessibility and feedback
- Added confirmation dialogs for destructive actions
- Improved HTMX feedback and accessibility labels

---

## [2.0.0] - 2026-03-17

### Added
- Phase 7: System hardening and polish
- Phase 6: UI Implementation
- Phase 5: Quality Profiles CRUD API
- Phase 4: Add library scanning (scan job type, API, CLI)
- Phase 3 & 4: Statistics/Dashboard + Metadata Enrichment
- Phase 3: Polish & Hardening
- Phase 2: UI Operational
- Artist tracking and scheduler implementation
- Disk quota service, quota alerts, and AcoustID score storage
- MCP tools — scan_library, add_library, list_monitored_artists, cancel_job, retry_job
- Cover art image caching and MIME detection
- Configurable cover art source priority per quality profile
- Total track count and source badge to watchlist preview

### Security
- Fixed privilege escalation in registration
- Fixed XSS in log streaming and secured WebSocket routes
- Fixed N+1 queries in watchlist filtering

### Infrastructure
- Unified server+worker in single container
- Migrated to pongo2 (Jinja2) templates
- Dynamic library routing via MUSIC_LIBRARY environment variable
- Parallel scanning with concurrent IO worker pool
