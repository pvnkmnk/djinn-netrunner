# Sentinel Security Journal

## 2025-05-15 - [Initial Security Review]
**Vulnerability:** Found XSS in WebSocket log streaming and missing authentication on WebSocket endpoints.
**Learning:** Fiber's WebSocket middleware doesn't automatically inherit group middleware unless explicitly applied. HTML snippets generated in backend for HTMX must be manually escaped.
**Prevention:** Always use `html.EscapeString` when embedding data into HTML snippets. Ensure all routes, including WebSockets, are covered by authentication middleware.

## 2025-05-22 - [WebSocket Broken Object Level Authorization (BOLA)]
**Vulnerability:** Any authenticated user could subscribe to any job's log stream or the global system event stream via WebSockets.
**Learning:** Authentication middleware only verifies identity, not authorization. WebSocket handlers must explicitly check user roles and object ownership (e.g., Job.OwnerUserID) to prevent BOLA.
**Prevention:** Always verify that the authenticated user has permission to access the specific resource requested via WebSocket parameters.

## 2025-05-29 - [Broken Object Level Authorization (BOLA) in Schedules]
**Vulnerability:** Authenticated users could create, update, or delete schedules for watchlists they did not own by guessing the schedule ID or providing a different watchlist ID.
**Learning:** Even when using session-based authentication, object-level checks must be performed by joining with the "owner" entity (e.g., Watchlist) to verify ownership before modifying resources.
**Prevention:** Always include ownership criteria in database queries (e.g., `.Joins("JOIN watchlists ...").Where("watchlists.owner_user_id = ?", user.ID)`) for sensitive operations.

## 2025-06-05 - [BOLA in Monitored Artists]
**Vulnerability:** Monitored artists could be listed, added, updated, or deleted by any authenticated user regardless of ownership.
**Learning:** Resource-level authorization must be enforced in both the API handlers and the underlying services by passing user identity/role context.
**Prevention:** Update service signatures to accept authorization context (userID, isAdmin) and ensure all GORM queries for that resource incorporate ownership filters.

## 2026-03-24 - [BOLA in Libraries and Quality Profiles]
**Vulnerability:** Libraries and Quality Profiles lacked owner tracking, allowing any authenticated user to view, modify, or delete resources belonging to others.
**Learning:** Fiber's `c.Locals("user")` should be used consistently across all protected handlers to eliminate redundant database session lookups and enable reliable authorization checks.
**Prevention:** Always include `OwnerUserID` in core resource models and apply ownership filters in GORM queries unless the user has an administrative role.

## 2026-03-28 - [BOLA in Watchlist Preview and Form]
**Vulnerability:** Watchlist previews and the edit form were accessible to any authenticated user by ID, potentially leaking source URIs and library contents.
**Learning:** Endpoints that only "read" or "preview" data are often overlooked for BOLA. Redundant manual session lookups should be refactored to use centralized middleware context to ensure consistent identity and authorization.
**Prevention:** Apply ownership filters (e.g., `.Where("owner_user_id = ?", user.ID)`) to all resource-fetching queries, even for previews and forms.
