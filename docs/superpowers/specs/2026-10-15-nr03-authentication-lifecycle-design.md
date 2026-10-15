# NR03 Authentication Lifecycle Design

**Issue:** DJI-563 — NR03: Verify compatible protocol and management authentication lifecycles  
**Branch:** `nr03-auth`  
**Date:** 2026-10-15

## Purpose

Define and verify secure server-side authentication lifecycles for third-party
Subsonic/OpenSubsonic clients and NETRUNNER management consumers. The work is
an audit-and-close milestone: it must describe the authentication modes the
server actually supports, close the high-confidence lifecycle gaps, and make
unsupported compatibility explicit.

This work does not implement a third-party client, replace the existing
identity system, or move cross-user authorization into NR03.

## Existing contract

### Browser and management sessions

- Account passwords are stored as bcrypt hashes.
- Successful login creates a cryptographically random `session_id` persisted
  in the `sessions` table.
- Sessions expire after seven days and carry user, IP, and user-agent data.
- The session is delivered as an HttpOnly, SameSite=Strict cookie; `Secure` is
  derived from the request scheme.
- Browser mutations use the existing `csrf_` double-submit cookie and CSRF
  header/form handling.
- Registration and login are rate-limited at the route layer.

NR03 must preserve these browser properties and must not make a management
credential a substitute for the browser CSRF boundary.

### Subsonic/OpenSubsonic

The current server recognizes the standard account-password path (`u` + `p`)
and the shared-password token/salt path (`u` + `t` + `s`). Both resolve a
server-side user before a protected endpoint proceeds. Shared token
authentication is disabled when its configured shared password is absent; the
empty-secret case must never degenerate into a forgeable `md5("" + salt)`
comparison.

Protocol responses must retain the existing Subsonic 200-with-error-envelope
behavior and versioned endpoint contracts. Authentication failures must not
become browser redirects or management JSON error envelopes.

## Design

### 1. Authentication inventory and compatibility matrix

Create a source-backed inventory of all advertised authentication modes and
routes:

- legacy account-password authentication
- token/salt authentication
- any OpenSubsonic api-key authentication currently implemented or advertised
- browser/management session authentication
- media, artwork, download, websocket/event, and relevant write endpoints

For each mode, record accepted parameters, mutually exclusive parameters,
validation rules, error code/status, principal resolution, expiry/revocation
behavior, and whether credentials may appear in a URL. The matrix must
distinguish implemented behavior from planned or unsupported behavior.

### 2. Principal and authorization boundary

Every accepted credential resolves to the existing server-side `User` record.
NR03 owns authentication and credential lifecycle; NR04 owns the subsequent
cross-user authorization policy. No endpoint may trust a client-supplied user
ID as proof of identity or ownership.

Subsonic handlers continue to use protocol-specific authentication middleware.
Management JSON routes continue to use session-cookie authentication. Browser
CSRF protections remain mandatory for browser-originated mutations.

### 3. Credential lifecycle

The implementation will prefer the existing persisted session lifecycle where
it can satisfy a management consumer without weakening browser security. If a
device/app credential is necessary for non-browser clients or event/media
requests, it will be a separate credential type with:

- cryptographically random one-time issuance material
- only a hash stored at rest
- an explicit user association and scope
- issued-at and expiry timestamps
- revocation and rotation behavior
- constant-time verification where applicable
- no recoverable copy of the primary account password

The design does not require OAuth, Hydra, or another external identity
provider. A new credential type must not be introduced solely to make a test
fixture easier.

### 4. Request and transport behavior

Authentication must be tested through the request shapes clients actually use:

- GET and POST protocol parameters where the endpoint contract allows both
- normal metadata and browse requests
- original media streams
- artwork and downloads
- websocket/event connections where applicable
- relevant authenticated writes

Long-lived privileged credentials must not be placed in URLs. Where the legacy
protocol requires query parameters, logs, diagnostics, and returned errors
must redact password, token, salt, and api-key values. The server must reject
malformed or conflicting credentials predictably rather than silently
choosing a weaker path.

### 5. Discovery and documentation

OpenSubsonic extension discovery will advertise only capabilities covered by
passing conformance tests. If api-key authentication is not implemented, the
server will not advertise it as supported. Documentation will explain how a
third-party user obtains usable credentials, what can be revoked, and any
compatibility limitations.

Named/versioned third-party client observations will be recorded alongside
headless tests. An unresolved client gap becomes an explicit follow-up rather
than a hidden first-party prerequisite.

## Verification strategy

Add table-driven tests and a headless lifecycle test covering:

- valid and invalid credential combinations
- malformed parameters and conflicting credentials
- disabled users
- expiry and revocation
- logout and concurrent sessions/devices
- restart persistence
- GET/POST parameter handling
- media/artwork/download/event authentication
- password/token redaction
- browser session and CSRF regressions
- persistence and configured registration policy

Run the package tests, full backend tests, `go vet`, the Postgres-gated tests,
and the full E2E suite. Any unsupported protocol mode must be represented by
an explicit negative test and documentation rather than a silent skip.

## Scope boundaries

In scope:

- server-side authentication behavior and lifecycle
- Subsonic/OpenSubsonic credential compatibility actually supported by this
  server
- management consumer login/identity/logout/expiry behavior
- credential persistence, revocation, redaction, and discovery
- conformance and interoperability evidence

Out of scope:

- third-party client implementation
- cross-user authorization policy assigned to NR04
- external identity infrastructure
- replacing working authentication without evidence
- unrelated API DTO or pagination changes already covered by NR02

## Completion criteria

NR03 is complete when the server's supported authentication modes are
documented and tested, accepted credentials have predictable expiry and
revocation behavior, browser CSRF/session behavior remains green, credentials
are not leaked, and unresolved compatibility gaps are explicit follow-up work.
