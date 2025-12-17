# UI Implementation (HTMX + Console)

This document defines the UI patterns and contracts for the NETRUNNER operations panel, including live console streaming and attach modes.

## Principles
- Console-first UX: logs are the progress visualization; avoid progress bars/spinners as primary UI.
- HTMX server-rendered partials: keep UI logic on the server; minimal JS only for console controls.
- Event-driven updates: use PostgreSQL LISTEN/NOTIFY + WebSockets; do not poll the database for console output.

## Page structure
The main page is a server-rendered shell with:
- A stats region updated via HTMX partial refresh.
- A playlists/sources region updated via HTMX partial refresh.
- A console region containing:
  - a socket container (swapped to attach/detach)
  - a scrollable log lines container
  - minimal controls (filter/copy/clear/live pause)

## Console socket pattern
A dedicated DOM region (e.g., #console-socket) is swapped to “attach” to a job without custom front-end routing.
- When jobid is present, render an element that opens a WebSocket (hx-ext=""ws"") to /ws/jobs/{jobid}.
- When jobid is absent, render an inert placeholder.

## Attach modes (STARTED vs ATTACHED)
Two attach modes balance liveness proof with spam prevention:
- STARTED: used immediately after starting a new job; sends a small backlog tail (e.g., last 50 lines).
- ATTACHED: used when attaching to an existing job; “quiet attach” since last seen log id (since-id).

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
- Filters: ALL / OK / INFO / ERR via data-filter attribute and CSS selectors.
- COPY LAST 200 copies recent rendered lines to clipboard.
- CLEAR clears viewport (does not mutate DB logs).

## Styling constraints
- Single CSS file.
- Minimal animation.
- No front-end component frameworks.
