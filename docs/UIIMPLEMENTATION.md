# UI Implementation (HTMX + Console)

This document defines the UI patterns and contracts for the NETRUNNER operations panel, including live console streaming and attach modes.

## Principles
- Console-first UX: logs are the progress visualization; avoid progress bars/spinners as primary UI.
- HTMX server-rendered partials: keep UI logic on the server; minimal JS only for console controls.
- Event-driven updates: use PostgreSQL LISTEN/NOTIFY + WebSockets for console output on the production stack; do not add UI polling loops. The NetrunnerLite SQLite mode may use worker polling instead of NOTIFY (see `docs/WHITEPAPER.md` deployment modes).

## Page structure
The main page is a server-rendered shell with:
- A stats region updated via HTMX partial refresh.
- A playlists/sources region updated via HTMX partial refresh.
- An artists region (CRUD via modal).
- A schedules region (cron editor + toggle).
- A libraries region.
- A jobs history region.
- A console region containing:
  - a socket container (swapped to attach/detach)
  - a scrollable log lines container
  - minimal controls (filter/copy/clear/live pause)

## Partials
- `GET /partials/stats` - Job statistics (queued, running, succeeded, failed counts in last 24h). Uses conditional aggregation for efficient single-query stats.
- `GET /partials/watchlists` - List of configured watchlists with status.

## Modal Pattern (Phase 6+)
HTMX forms use server-side modal triggering:
- Server sets `HX-Trigger: openModal` header in form load response
- Client JS listens globally for `htmx:afterOnLoad` and opens the modal
- No inline `hx-on:htmx:after-request` handlers on buttons

## Console socket pattern
A dedicated DOM region (#console-socket) is swapped to "attach" to a job without custom front-end routing.
- `#console-job-picker` lists the caller's queued and running jobs - their own, or everyone's for an admin - from `GET /partials/console-jobs`, refreshing on load and every 30 seconds, and echoing the current selection back as `selected` so a refresh cannot reset it.
- The Attach button posts the picked `job_id` to `POST /console/attach`.
- When the job resolves and the caller may stream it, the reply renders the socket element with `hx-ws="connect:/ws/jobs/{jobid}?tail=200"`; otherwise it renders an inert placeholder that says why, and no socket element at all.
- The socket is opened by htmx's in-core `hx-ws` attribute, not `hx-ext="ws"`: this app ships htmx 1.9.10 core, which has no `ws` extension, so `ws-connect` was inert and the console never connected. (`hx-ws` is deprecated upstream in favour of the extension; if the extension is ever vendored, this note and the templates move with it.)
- Every frame the server sends carries `hx-swap-oob="beforeend:#console-logs"`: in-core ws swaps each top-level element of a message into the element carrying the same id, so a frame has to name the pane, and appending is what a log stream is. The attach reply clears that pane out of band, so two jobs' lines cannot mix.

## Attach modes (STARTED vs ATTACHED)
Two attach modes balance liveness proof with spam prevention:
- STARTED: used immediately after starting a new job; sends a small backlog tail (e.g., last 50 lines).
- ATTACHED: used when attaching to an existing job; "quiet attach" since last seen log id (since-id).

## WebSocket log streaming contract
- ops-web exposes /ws/jobs/{jobid}.
- On connect:
  - If tail is requested: send last N lines.
  - Else if since-id is requested: send lines after id.
  - Else: send a small default backlog.
- Keep the socket open and stream new log events.

Preferred server-side mechanism:
- LISTEN to a NOTIFY channel (e.g., opsevents) and fan out log updates to connected WebSocket clients.

## Console controls (minimal JS)
- Auto-scroll pauses when the operator scrolls up; resumes when bottom is reached.
- RESUME LIVE forces scroll-to-bottom and resumes follow mode.
- Filters: ALL / OK / INFO / WARN / ERR via data-filter attribute and CSS selectors.
- COPY LAST 200 copies recent rendered lines to clipboard.
- CLEAR clears viewport (does not mutate DB logs).

## Styling constraints
- Single CSS file.
- Minimal animation.
- No front-end component frameworks.
