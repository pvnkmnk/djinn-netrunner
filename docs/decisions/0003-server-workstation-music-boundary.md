# ADR 0003: Server–Workstation Music Boundary

## Status

**Accepted for documentation and fixture-only validation on 2026-08-17.** This decision establishes ownership and safe handoff rules only. It does not authorize a music-data migration, service deployment, acquisition action, credential activation, storage change, backup execution, or modification of CT 100.

## Context

The personal music environment has several useful but materially different data domains. The server supports the listening and streaming experience, while the workstation supports inbox triage, production projects, samples, stems, masters, works in progress, and DJ preparation. Treating those domains as one undifferentiated collection would make a local organizational action look like authority to change server content, or make a streaming service look like authority over irreplaceable production assets.

The portfolio ownership map already distinguishes the server listening library from the workstation production-vault hierarchy and describes service indexes, caches, and runtime databases as derived state. It also identifies `djinn-netrunner` as the candidate server-side operations runtime and `music-library-manager` as the approved local companion for deliberate workstation review. The local manager’s strict zones provide the concrete workstation-side vocabulary for this boundary.

## Decision

### 1. Canonical ownership

| Data class | Canonical owner | Permitted responsibility | Explicitly not authoritative |
|---|---|---|---|
| Listening-library content deliberately retained for server playback | **Server music store** | Retains the approved listening collection and supports private streaming clients | Navidrome/Gonic indexes, scan databases, player history, caches, and a local review copy |
| Server acquisition staging and job provenance | **Server music operations runtime** | Holds a bounded, auditable staging or job surface only when a later operation is separately approved | The workstation inbox, production hierarchy, or a streaming index |
| Streaming and scan state | **Derived service state** | May be regenerated from declared service configuration and the canonical listening content | A replacement for the listening-library source of truth |
| Workstation inbox | **Private workstation music hierarchy** | Holds untriaged local files pending deliberate classification | An implicit server intake path or automatic synchronization source |
| Workstation production projects, samples, stems, masters, WIPs, and mix-preparation material | **Private workstation production-vault hierarchy** | Supports local creative work and DAW-specific organization | The server listening library, server acquisition staging, or streaming-service media root |
| DJ sets and long-form mixes | **Private workstation DJ/mix hierarchy** | Retains mix assets and their local index | A general-purpose listening-library import queue |
| Metadata, dedupe, and analysis records | **Context-specific derived records** | Support review in the system that created them | Evidence that the underlying media may be deleted, moved, or overwritten |

### 2. Direction of authority

The server music store is the sole canonical owner of content intentionally retained for the server listening experience. `djinn-netrunner` may eventually coordinate approved server-side jobs over that scope, but this ADR does not enable any acquisition provider, import, scan, move, or delete operation.

The workstation is the sole canonical owner of its inbox, production hub, sample collection, DAW projects, stems, masters, works in progress, and DJ/mix hierarchy. `music-library-manager` may inspect and propose transformations in those zones only. Its `01_Listening_Library` zone is a local review/organization surface; it is not an implicit bidirectional replica of the server library and does not create authority to alter server-owned content.

A copy in either domain does not establish a second source of truth. It must be treated as a deliberately scoped working copy with known provenance, a stated purpose, and a documented disposition before any consequential change is proposed.

### 3. Allowed and prohibited handoffs

| Proposed handoff | Policy | Required record before any execution |
|---|---|---|
| Server acquisition staging → server listening library | **Potentially allowed later** | Approved source/rights posture, manifest, preview, duplicate policy, recovery evidence, and exact operation approval |
| Workstation inbox → workstation listening-review zone | **Potentially allowed later** | Classification result, duplicate check, preview, recovery source, and exact local approval |
| Workstation production asset → workstation production sub-zone | **Potentially allowed later** | Local preview, conflict rule, recovery source, and exact local approval |
| Server listening library ↔ workstation review copy | **No implicit sync** | A separate item-scoped transfer decision that names direction, provenance, ownership after transfer, conflict handling, and rollback |
| Samples, stems, DAW projects, masters, WIPs, or mixlists → server listening library | **Prohibited by default** | A future, explicit policy change and a separately approved item-scoped operation |
| Server listening-library content → production asset location | **Prohibited by default** | A future, explicit policy change and a separately approved item-scoped operation |
| Any media domain → public or third-party distribution | **Out of scope** | Separate owner decision covering rights, destination, and data handling |

### 4. Metadata, duplicate, and manual-correction policy

Metadata is a property of the media item in its canonical domain. A metadata correction must identify the item, canonical domain, intended fields, evidence or rationale, and expected result. A scan cache, fingerprint database, or player index may inform a correction but is not itself an authority to overwrite canonical metadata.

Exact hash matches and near-duplicate signals are review inputs, not deletion authority. They may produce a report and a proposed keep rule, but they must not trigger cross-domain deletion, overwrite, or consolidation. The later safety contract defines the required preview, recovery, approval, and audit sequence for any consequential change.

A manual correction is permitted only as a proposed, item-scoped correction until it receives exact approval. The record must state whether the correction affects server listening content, a workstation local asset, or a derived record; propagation to another domain is never presumed.

### 5. Private access and system boundaries

The server listening library remains private-access only. Streaming reachability is separate from administrative access, and player or service authentication does not grant authority over host configuration, music ownership, or workstation assets.

Secrets, credentials, addresses, mounts, raw media listings, and private-vault content are outside this ADR. No credential, service, or network change is authorized by the decision.

## Consequences

This decision makes the Phase 2 contract sequence explicit: the music data-safety and recovery contract must be applied before a consequential move, tag, dedupe, import, centralization, or synchronization is proposed. It also means that slskd, Navidrome changes, service-media placement, and any synchronization remain separate work with separate approvals.

The decision intentionally leaves current physical paths, runtime topology, media contents, backup implementation, and availability of an independent off-host backup destination unspecified. Those facts must be observed or separately approved; this ADR does not infer them from repository capabilities or historical configuration.

## Verification and review

The decision can be reviewed without live media by checking that fixture-only tests and documentation classify each fictional item into exactly one canonical data domain, reject unapproved cross-domain transfers, and retain derived-state labels. A later operational rehearsal must use the safety contract, an approved fixture or item manifest, and exact owner approval.

