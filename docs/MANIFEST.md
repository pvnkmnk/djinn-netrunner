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
- docs/OPENSUBSONIC_COVERAGE.md — generated Subsonic/OpenSubsonic endpoint matrix, the pinned spec revision, and which gaps belong to which ticket.
- docs/SECRET_SCAN_FINDINGS.md — a triaged full-history secret scan: what the 15 findings are, why 14 of them are not leaks, and whether to add a CI gate.
- docs/SECRETS_MANIFEST.md — every credential the project needs, its form, and the exact file (and key) it belongs in; the checklist for supplying or rotating one.
- docs/RUNBOOK.md — day-to-day operations and troubleshooting.

### Operators
- docs/RUNBOOK.md — operational procedures and failure modes.
- docs/DEPLOYMENT.md — bringing up the dev or release deployment from a clone.
- CHANGELOG.md — what changed in each release, newest first.

### History
- docs/project-history.md — how the project got here, wave by wave, with PR links.
- docs/BETA_ACCEPTANCE.md — the acceptance matrix for the retired beta path: the output each row observed, kept as written rather than as a bring-up guide.
- docs/plans/ — dated point-in-time records (plans, gap lists, release checklists) kept as written, never as instructions; see docs/plans/HISTORICAL.md.

## What to read when
- Install: README.md, docs/RUNBOOK.md
- Understand architecture: docs/WHITEPAPER.md, docs/ARCHITECTURE.md
- Modify UI: docs/UIIMPLEMENTATION.md
- Change the Subsonic/OpenSubsonic surface: docs/OPENSUBSONIC_COVERAGE.md (regenerate it with `py scripts/opensubsonic_coverage.py <openapi.json> docs/OPENSUBSONIC_COVERAGE.md`, never hand-edit)
- Investigate a secret-scan finding, or gate CI on one: docs/SECRET_SCAN_FINDINGS.md
- Add or rotate a credential, or wire secrets into a new target: docs/SECRETS_MANIFEST.md, then docs/DEPLOYMENT.md
- Add features/job types: AGENTS.md, docs/ARCHITECTURE.md
- Debug prod issues: docs/RUNBOOK.md, then docs/ARCHITECTURE.md

## Keeping docs updated
- Update docs/ARCHITECTURE.md when adding services, changing concurrency, or modifying schema/locks.
- Update docs/UIIMPLEMENTATION.md when changing HTMX patterns, attach modes, or WebSocket streaming.
- Update docs/RUNBOOK.md when new failure modes or operational procedures are discovered.
- Update CHANGELOG.md in the same PR that changes behaviour: the `[Unreleased]` section
  is the only place a release is described, and it is what an operator reads.
- Update docs/SECRET_SCAN_FINDINGS.md in the same PR that adds a secret scan to CI,
  changes the scan result, or allowlists a finding.
- Update docs/SECRETS_MANIFEST.md whenever a credential's destination, form, or
  required-ness changes; it is the file an operator follows when a secret is missing.
- Add a dated section to docs/project-history.md at the end of a wave, linking the PRs.
  It is the only narrative record of why a change was made.
