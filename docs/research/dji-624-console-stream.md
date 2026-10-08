# DJI-624 — Is the live console log stream still pointed at a route that does not exist?

**Verdict: still live.** Both halves are true, and they are separate findings:

- **(a) The client points at a route that does not exist.** `ws-connect="/ws/jobs"` has no matching route. **Still live.**
- **(b) The route that does exist is unreachable from the UI.** `GET /ws/jobs/:job_id` is registered and its handler works, but nothing in the page ever supplies a job id, so the console is dead regardless of (a). **Still live**, and it is the larger of the two.

`ops/web/codemap.md:16` and `ops/web/templates/codemap.md:23` also still claim the non-existent path, and `backend/internal/api/codemap.md:56` names a third path that never existed either.

## Evidence

Client target — `ops/web/templates/index.html:102`:

```
<div id="console-logs" class="console-logs" hx-ext="ws" ws-connect="/ws/jobs" aria-live="polite">
```

Registered WebSocket routes — `backend/cmd/server/main.go:449-452` (the only two in the file):

```
app.Get("/ws/events", auth.AuthMiddleware, websocket.New(ws.HandleEvents))
app.Get("/ws/jobs/:job_id", auth.AuthMiddleware, websocket.New(func(c *websocket.Conn) {
    ws.HandleConsole(c, db)
}))
```

There is no bare `/ws/jobs`. Confirmed by reading every `app.Get`/`Group` in `main.go`; `/ws/` appears only as a path prefix at `backend/internal/api/browser.go:31` (`machinePathPrefixes`), which is a refusal-classification list, not a route.

The handler exists and needs a path param — `backend/internal/api/websocket.go:164-166`:

```
func (m *WebSocketManager) HandleConsole(c *websocket.Conn, db *gorm.DB) {
	jobIDStr := c.Params("job_id")
	jobIDUint, _ := strconv.ParseUint(jobIDStr, 10, 64)
```

So even if a bare `/ws/jobs` route were added pointing at `HandleConsole`, `job_id` would be empty, `ParseUint` would fail with `jobIDUint == 0`, and `db.First(&job, 0)` at `websocket.go:175` would answer `"Job not found"` and close. The route is not merely missing.

The codebase already knows — `backend/cmd/server/main.go:295-302`:

```
// Console attach. Nothing selects a job yet (DJI-562: the console
// streams /ws/jobs/:job_id and the page never names one), so this
// reports the state honestly instead of replying with a constant
// that reads like an instruction. The button targets #console-socket,
```

The console section renders on the authenticated dashboard only — `ops/web/templates/index.html:5` `{% if authUserID == "" %}` / `:55` `{% else %}`; the `#console-logs` div at `:102` is inside the `else` branch, so it is present for any signed-in user on `/`.

Stale docs: `ops/web/codemap.md:16` `4. WebSocket at `/ws/jobs` streams job logs to console`; `ops/web/templates/codemap.md:23` `- **WebSocket**: `/ws/jobs` streams log lines to console div`; `backend/internal/api/codemap.md:56` `1. Client connects to `/ws/console/:job_id` or `/ws/events``.
Correct docs: `AGENTS.md:230`, `docs/ARCHITECTURE.md:75-76`, `backend/cmd/server/codemap.md:12`.
`docs/UIIMPLEMENTATION.md:35` describes the intended form (`to /ws/jobs/{jobid}`), which is what the route table actually has.

## Is it reachable at runtime?

The htmx **ws extension is present in the shipped bundle** — `ops/web/static/js/htmx.min.js` is htmx `version:"1.9.10"` (single minified line) and contains the connect path. Its spec parser splits the attribute value on `:` looking for `connect`, and the connect function (`xt` in the bundle) does:

- `if(r.indexOf("/")==0)` → prefix `location.hostname` and `:port`, choosing `ws://` or `wss://` by page protocol
- `Q.createWebSocket(r)` → `new WebSocket(r, [])`
- `t.onerror = ... fe(s,"htmx:wsError",...)`
- `t.onclose = ... if([1006,1012,1013].indexOf(e.code)>=0){ setTimeout(xt, backoff) }`

So on dashboard load the browser attempts `ws://<host>/ws/jobs`. That path is under the `/ws/` machine-prefix, which answers a JSON 404 rather than an HTML page; the handshake fails and no log line ever arrives. `#console-logs` stays empty — and because it carries `aria-live="polite"` with no content, there is no visible or announced feedback at all.

**Worth flagging:** a failed handshake reports close code 1006, which is in the retry set, so the extension reconnects with full-jitter backoff indefinitely — a permanent reconnect loop against a 404, not a single dead request. I inferred the 1006 code from the spec's abnormal-closure definition; see *Could not verify*.

## Would an existing test have caught it?

**No.** `backend/cmd/server/main_test.go:92` lists `"GET /ws/events"` as the only WebSocket entry in the critical-route table. Neither `/ws/jobs` nor `/ws/jobs/:job_id` is asserted anywhere, so neither a missing bare route nor a renamed one fails CI.

`backend/internal/api/templates/ui_honesty_test.go:48` (`TestAttachReplyLandsInTheConsoleRegionNotTheButton`) reads `index.html` and asserts the **Attach button's** `hx-target` and `aria-live` — it never looks at `ws-connect`. The template package has no assertion naming any WebSocket URL.

## Cheapest assertion that would

Two, because the findings are two:

1. A route-table assertion in `backend/cmd/server/main_test.go`, using the `routeTable` helper already at `:48`, that every `ws-connect` target in `ops/web/templates` is a registered `GET` route. That is the general guard: it catches this class rather than this instance.
2. A plain route assertion that `"GET /ws/jobs/:job_id"` is registered, so a rename cannot silently orphan the handler.

A one-off `assert.Contains(page, "ws-connect=\"/ws/jobs\"")` would pin the defect rather than the contract — it would pass forever while the console stays dead — so it is the wrong shape.

## Disposition

**Fix now** — routing the console is the actual work, and DJI-562's own scope is the missing route. The fix is not "add the route": it is a job selector. Either the console takes a job id (the route already exists; the page must name one) or it streams a per-user stream the way `/ws/events` streams system events. Per `main.go:295`, the honest-attach behaviour was a deliberate stopgap for exactly this gap.

Cheap partial worth doing independently: correct the three stale docs (`ops/web/codemap.md:16`, `ops/web/templates/codemap.md:23`, `backend/internal/api/codemap.md:56`). They are wrong in three different ways today and they are what a reader consults first.

## What would falsify this verdict

- A route registered at runtime that is not visible as an `app.Get` literal in `main.go` — e.g. a loop or a group registering `/ws/jobs`. I read `main.go` for `/ws/` literals and found only two; a dynamic registration would defeat that.
- A reverse proxy or Caddy rewrite mapping `/ws/jobs` → `/ws/jobs/:job_id` before the request reaches Fiber. Checked `ops/caddy/Caddyfile` is **not** part of what I verified.
- A second `#console-logs` element (or a JS call) setting `ws-connect` to a real job URL at runtime from `ops/web/static/js/app.js`. `app.js:292` references `console-logs` — I saw the reference but not its full body, so this is the strongest falsifier and belongs at the top of any follow-up.

## Could not verify

- **Whether anything in `ops/web/static/js/app.js` rewrites `ws-connect` at runtime.** `app.js:292` mentions `console-logs`; I did not read that region. If it does rewrite the attribute with a job id, finding (b) is narrower than stated and htmx's `htmx:load` processing may connect correctly after the rewrite. Priority check.
- **The observed close code.** 1006 is the spec's abnormal closure and is in the extension's retry set, but I read that from the minified bundle rather than observing a browser.
- **The exact trigger by which the ws extension picks up `ws-connect`** (I saw the connect function and the `onerror`/`onclose` wiring; the registration hook above it was truncated by the output limit).
- I did not run the stack, so no observed 404 and no DevTools evidence.
