# Documentation Manifest

Purpose: This file lists documentation in the Djinn NETRUNNER repository and explains when to read each doc.

## Entry points
### First-time setup
- README.md — installation steps and prerequisites.
- docs/WHITEPAPER.md — product intent and production posture.
- docs/ARCHITECTURE.md — system architecture and invariants.
- docs/RUNBOOK.md — day-to-day operations and troubleshooting.

### Developers
- AGENTS.md — agentic IDE guidelines, constraints, and "do not break" rules.
- docs/ARCHITECTURE.md — worker model, locks, tables, and correctness contracts.
- docs/UIIMPLEMENTATION.md — HTMX patterns, console streaming, attach modes, minimal JS contract.
- docs/RUNBOOK.md — day-to-day operations and troubleshooting.

### Operators
- docs/RUNBOOK.md — operational procedures and failure modes.

## What to read when
- Install: README.md, docs/RUNBOOK.md
- Understand architecture: docs/WHITEPAPER.md, docs/ARCHITECTURE.md
- Modify UI: docs/UIIMPLEMENTATION.md
- Add features/job types: AGENTS.md, docs/ARCHITECTURE.md
- Debug prod issues: docs/RUNBOOK.md, then docs/ARCHITECTURE.md

## Keeping docs updated
- Update docs/ARCHITECTURE.md when adding services, changing concurrency, or modifying schema/locks.
- Update docs/UIIMPLEMENTATION.md when changing HTMX patterns, attach modes, or WebSocket streaming.
- Update docs/RUNBOOK.md when new failure modes or operational procedures are discovered.
