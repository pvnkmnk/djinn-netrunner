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

## 2025-06-05 - [Broken Object Level Authorization (BOLA) in Artists]
**Vulnerability:** Authenticated users could list, update, or delete monitored artists belonging to other users because the application lacked ownership checks in both the API handlers and the `ArtistTrackingService`.
**Learning:** High-level services that manage user-owned entities must support multi-tenancy by accepting and filtering by `OwnerUserID`. UI-rendering handlers (HTMX partials) must also be secured as they can be called directly.
**Prevention:** Update service methods to accept `userID` and `role` for filtering. Ensure all handlers (JSON and HTML/HTMX) explicitly verify ownership or use filtered service methods.
