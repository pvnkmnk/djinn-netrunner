## 2025-05-15 - WebSocket Authentication Gap
**Vulnerability:** WebSocket endpoints at `/ws/events` and `/ws/jobs/:job_id` lacked authentication, allowing unauthenticated users to view system events and job logs.
**Learning:** Fiber's `websocket` middleware is often defined separately from main API groups, making it easy to forget applying global authentication middleware to these routes.
**Prevention:** Always group WebSocket routes under an authenticated group or explicitly apply `AuthMiddleware` to each WebSocket route definition.
