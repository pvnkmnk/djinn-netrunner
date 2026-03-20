# Sentinel Security Journal

## 2025-05-15 - [Initial Security Review]
**Vulnerability:** Found XSS in WebSocket log streaming and missing authentication on WebSocket endpoints.
**Learning:** Fiber's WebSocket middleware doesn't automatically inherit group middleware unless explicitly applied. HTML snippets generated in backend for HTMX must be manually escaped.
**Prevention:** Always use `html.EscapeString` when embedding data into HTML snippets. Ensure all routes, including WebSockets, are covered by authentication middleware.

## 2025-05-22 - [WebSocket Broken Object Level Authorization (BOLA)]
**Vulnerability:** Any authenticated user could subscribe to any job's log stream or the global system event stream via WebSockets.
**Learning:** Authentication middleware only verifies identity, not authorization. WebSocket handlers must explicitly check user roles and object ownership (e.g., Job.OwnerUserID) to prevent BOLA.
**Prevention:** Always verify that the authenticated user has permission to access the specific resource requested via WebSocket parameters.
