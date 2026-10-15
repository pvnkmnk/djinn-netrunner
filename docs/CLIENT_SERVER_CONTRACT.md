# NETRUNNER client/server contract

This document describes the contract for clients that connect to an existing
NETRUNNER appliance. It is a reference for client and server development, not
a promise that every endpoint in the OpenSubsonic specification is implemented.

The contract has two deliberately separate surfaces:

1. the standard Subsonic/OpenSubsonic listening surface for third-party music
   clients; and
2. NETRUNNER's own authenticated management surfaces for acquisition, jobs,
   libraries, monitoring and operations.

A client may use both surfaces, but standard listening features belong on the
standard protocol endpoints. NETRUNNER-specific operations must not be
advertised as if they were standard Subsonic features.

## Source of truth and versioning

The current protocol audit is pinned to OpenSubsonic **1.16.1**:

- Specification: <https://opensubsonic.netlify.app/docs/openapi/openapi.json>
- Specification SHA-256:
  `cb54c03c33835d132c555863e9771e30dfaa2930312853ca27dfece2ed46bfb6`
- Complete endpoint and extension matrix:
  [`OPENSUBSONIC_COVERAGE.md`](OPENSUBSONIC_COVERAGE.md)
- Matrix generator: [`scripts/opensubsonic_coverage.py`](../scripts/opensubsonic_coverage.py)
- Route-table guard:
  [`backend/cmd/server/opensubsonic_coverage_test.go`](../backend/cmd/server/opensubsonic_coverage_test.go)
- Management API baseline schema:
  [`docs/api/client-contract.yaml`](api/client-contract.yaml), version `1.0.0`

The matrix is generated from the pinned specification and cross-checked against
the registered route table. This document explains how to consume the current
contract; it does not duplicate the 87-row inventory. When the specification
revision changes, update the version and digest together, regenerate the matrix,
review classifications and update this document's compatibility notes.

## Surfaces

### Standard listening surface

When Subsonic is enabled, standard endpoints are registered below `/rest` and
use the `.view` suffix:

```text
/rest/ping.view
/rest/getIndexes.view
/rest/getMusicDirectory.view
/rest/getArtist.view
/rest/getAlbum.view
/rest/getSong.view
/rest/search3.view
/rest/getAlbumList2.view
/rest/getRandomSongs.view
/rest/getCoverArt.view
/rest/stream.view
/rest/getScanStatus.view
/rest/startScan.view
/rest/getPlaylists.view
/rest/getPlaylist.view
/rest/createPlaylist.view
/rest/deletePlaylist.view
/rest/getLicense.view
```

`/rest/license.view` remains as a compatibility alias for
`/rest/getLicense.view`; it is not a separate capability. Subsonic is
configuration-gated, so a disabled deployment does not register `/rest` routes.

The complete matrix is authoritative for what is implemented, what is tested,
and what remains a gap. A route existing is not sufficient evidence of protocol
conformance: its parameters, response shape, authorization and error behavior
must also be verified.

### NETRUNNER management surface

The management surface is separate from Subsonic:

- **HTTP/JSON API:** `/api/*` for authentication, watchlists, artists,
  libraries, schedules, jobs, profiles, acquisition and administration.
- **Server-rendered UI:** authenticated pages under `/` and HTMX partials under
  `/partials/*`.
- **WebSockets:** `/ws/jobs/:job_id` for a selected job's log stream and
  `/ws/events` for the authorized system event stream.
- **CLI:** `netrunner-cli` for status, configuration, watchlists, libraries,
  profiles and statistics.
- **MCP:** `backend/cmd/agent` exposes stable read-only probes and stateful
  management tools for agents.

The management API uses the contracts documented in
[`ARCHITECTURE.md`](ARCHITECTURE.md) and its handler/service tests. It is not a
replacement for standard listening endpoints and should not be required by a
generic Subsonic client just to browse or play music.

The first versioned management baseline is published as an OpenAPI document at
[`docs/api/client-contract.yaml`](api/client-contract.yaml). It covers the
health, authentication, playlist, job-action and acquisition operations whose
JSON shapes are currently stable and tested. Additional management resources
must be added there before being treated as part of the versioned client
contract; existing HTML/HTMX, CLI and MCP consumers remain supported while the
catalogue expands.

Management list endpoints use bounded offset pagination when they expose
`page` and `limit`: page numbers start at 1, the default page size is 50, and
the maximum page size is 100. Invalid or oversized values return the shared
`invalid_pagination` JSON error. Resource identifiers remain opaque UUID strings
or positive decimal job IDs according to the route; clients must not infer one
identifier format from another.

## Authentication and authorization

### Subsonic clients

Every enabled `/rest/*.view` endpoint runs the Subsonic authentication
middleware. Requests identify the account with `u`, which is the user's email
address. A client authenticates with one of these forms:

- account-password authentication: `u` plus `p`;
- shared-password token authentication: `u`, `t` and `s`, where `t` is the
  lowercase hexadecimal MD5 of `SUBSONIC_PASSWORD + s`.

Token authentication is refused when `SUBSONIC_PASSWORD` is not configured;
accepting `md5(s)` in that case would allow anyone to forge a valid token.
Account-password authentication remains available without the shared password.
Subsonic authentication failures are returned in the protocol response envelope
with status `failed` and error code `40`, rather than as an HTML login page.

Clients must URL-encode the username. In particular, an email address containing
`+` must send `%2B`, because an unescaped plus is decoded as a space in query
parameters.

The authenticated user is placed in request context before a handler runs.
Browsing, search, artwork, streaming and playlist reads are therefore scoped to
the caller's visible library and playlist permissions. Tests covering these
boundaries live beside the Subsonic handlers; do not infer authorization from a
successful response for one fixture user.

### Management clients

The management surface uses session-cookie authentication. Protected pages,
HTMX partials and `/api/*` groups require a valid `session_id` cookie. Roles are
checked at the handler/service boundary; `admin` grants administrative actions,
while ordinary users remain owner-scoped.

`POST /api/auth/register` creates an account and does not issue a session.
The client then uses `POST /api/auth/login` to establish a server-side
session and issue the HttpOnly, SameSite=Strict `session_id` cookie (login is
also allowed to return the browser redirect form). `GET /api/auth/me` is the
management identity probe: with a live session it returns `id`, `email`, `role`
and `expires_at`, and never returns a password hash, raw session id, or device
token. Missing, expired, revoked, or disabled sessions return the standard
management JSON error (`401 {"error":"not authenticated"}`).

`POST /api/auth/logout` revokes only the presented session in the database and
clears the cookie. Reusing that cookie then fails for identity, management,
media, and websocket requests; other concurrent sessions remain independent.
Session expiry is server-enforced on every request. There is no separate device
token or API-key management authentication surface; clients must use the
session-cookie lifecycle. The readable `csrf_` cookie and `X-CSRF-Token` header
remain required for browser/API mutations, while GET identity is not a CSRF
mutation.

Mutating browser/API requests use the double-submit CSRF contract: the readable
`csrf_` cookie is echoed in `X-CSRF-Token`. A management client should obtain a
fresh CSRF token and send it with every deliberate mutation. Subsonic requests
use their protocol authentication and are not browser-session/CSRF requests.

## Response and request conventions

### Subsonic envelopes

Subsonic responses use the protocol envelope and advertise:

```xml
<subsonic-response status="ok" version="1.16.1" type="netrunner" />
```

JSON is available through the normal Subsonic response negotiation and is
wrapped in a `subsonic-response` object. Failures retain the protocol envelope:

```xml
<subsonic-response status="failed" version="1.16.1" type="netrunner">
  <error code="40" message="Authentication failed" />
</subsonic-response>
```

Clients should treat the protocol status and error object as authoritative; a
transport-level `200` does not mean the operation succeeded.

### IDs and pagination

IDs returned by browsing and search are opaque client values. Carry them forward
to `getMusicDirectory`, `getArtist`, `getAlbum`, `getSong`, `getCoverArt` and
`stream`; do not reconstruct database UUIDs or filesystem paths. Endpoints that
support pagination expose their Subsonic parameters and return the matching
protocol collection shape. The matrix and handler tests identify the currently
proven pagination operations.

### Streaming

`/rest/stream.view` is the standard playback endpoint. A client should request
the track ID returned by the browsing surface, send a `Range` header when it
needs seeking, and honor the returned status, content type and range metadata.
The route is present and smoke-tested, while the dedicated conformance coverage
for ranges, authorization and failure shapes is owned by NR07 / DJI-567.

The management API's `/tracks/:id/stream` route is a separate authenticated
library operation. Passing a management-route ID to a Subsonic client is not a
portable substitute for `/rest/stream.view`.

## Supported listening journeys today

The currently proven journey is:

1. authenticate with a Subsonic-compatible credential;
2. call `ping` and, if needed, `getLicense`;
3. browse artist indexes and directories with `getIndexes` and
   `getMusicDirectory`;
4. resolve artist, album and song metadata with `getArtist`, `getAlbum` and
   `getSong`;
5. use `search3`, `getAlbumList2` or `getRandomSongs` to discover additional
   tracks;
6. request artwork with `getCoverArt`;
7. play the returned song ID through `stream`; and
8. read or change playlists with the implemented playlist operations.

Library scanning status and scan requests are also exposed through
`getScanStatus` and `startScan`, subject to the caller's authorization.

This is a tested subset, not the final product boundary. The coverage matrix
records the remaining standard gaps and their owners, including browsing and
search breadth (NR06), streaming conformance (NR07), lyrics (NR22), radio
(NR23), sharing (NR24), richer artist/album information (NR25), podcasts (NR26),
chat (NR27) and jukebox control (NR28). An owned gap may close by implementing
the feature or by documenting an explicit unsupported-by-design decision.

## Compatibility and scope decisions

- The server remains client-agnostic. A first-party client is not the measure of
  protocol completeness.
- Standard protocol features stay on standard endpoints. NETRUNNER acquisition,
  job, library and settings operations remain management APIs.
- `/rest` support is configuration-gated and is available on both PostgreSQL
  and NetrunnerLite SQLite deployments; the edition boundary is documented in
  [`NETRUNNERLITE.md`](NETRUNNERLITE.md).
- Unsupported or unproven protocol features must not be advertised as supported
  through capability discovery.
- Podcast, chat and jukebox are explicit scope decisions in the project backlog,
  not accidental omissions from the matrix. Their tickets may still conclude
  with a deliberate unsupported decision and evidence.
- Native playback UI, device integrations, packaging and local-server
  provisioning are outside this server contract.

## Evidence and change policy

When changing a client-visible contract:

1. update the route/handler or service implementation;
2. add or update a focused contract test for request shape, response shape,
   authorization and error behavior;
3. update the generated coverage source rather than hand-editing its matrix;
4. run the route-table and relevant protocol tests;
5. verify both XML and JSON behavior when the endpoint supports both; and
6. update this document only when the boundary, authentication model, supported
   journey or compatibility decision changes.

The minimum evidence for a new listening capability is a route-table assertion,
a handler/service contract test, an authorization-negative test and a
representative client journey. A route count or happy-path smoke request alone
does not establish conformance.

## Related references

- [`ARCHITECTURE.md`](ARCHITECTURE.md) — service, API, security and runtime
  contracts.
- [`OPENSUBSONIC_COVERAGE.md`](OPENSUBSONIC_COVERAGE.md) — generated endpoint
  and extension matrix.
- [`NETRUNNERLITE.md`](NETRUNNERLITE.md) — PostgreSQL/SQLite edition boundary.
- [`DEPLOYMENT.md`](DEPLOYMENT.md) — deployment and operational setup.
- [`backend/internal/api/subsonic.go`](../backend/internal/api/subsonic.go) —
  current Subsonic handler implementation.
- [`backend/cmd/server/main.go`](../backend/cmd/server/main.go) — route
  registration and configuration gating.
- [`api/client-contract.yaml`](api/client-contract.yaml) — versioned
  management API baseline.
