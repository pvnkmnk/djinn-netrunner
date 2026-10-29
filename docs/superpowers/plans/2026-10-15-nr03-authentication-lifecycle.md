# NR03 Authentication Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Verify and close the server-side authentication lifecycle for management consumers and supported Subsonic/OpenSubsonic clients without weakening browser sessions, CSRF, or protocol envelopes.

**Architecture:** Keep browser and management authentication on the existing DB-backed `session_id` lifecycle, adding the missing JSON identity surface and explicit account disablement/revocation checks. Keep Subsonic credential verification protocol-specific, strict about conflicting modes, and explicit about unsupported api-key authentication; do not introduce a separate device-token subsystem unless implementation evidence proves sessions cannot satisfy the required lifecycle.

**Tech Stack:** Go 1.27, Fiber v3, GORM, SQLite/PostgreSQL, bcrypt, Subsonic XML/JSON envelopes, Go integration tests, Playwright E2E.

**Spec:** `docs/superpowers/specs/2026-10-15-nr03-authentication-lifecycle-design.md`

## Global Constraints

- Preserve HttpOnly, SameSite=Strict browser session cookies and the existing `csrf_` double-submit boundary.
- Preserve Subsonic 200 responses containing protocol error envelopes; do not return browser redirects or management `APIError` envelopes from `/rest`.
- Every accepted credential resolves to the existing server-side `database.User`; do not trust client-supplied user IDs.
- Do not store recoverable primary account passwords or introduce OAuth/Hydra/external identity infrastructure.
- Do not advertise OpenSubsonic api-key authentication unless it is implemented and covered by conformance tests.
- NR04 owns cross-user authorization; NR03 only establishes the authenticated principal and credential lifecycle.
- Use `env -u ENVIRONMENT -u CONFIG_ENV -u PORT` for host Go tests in this environment.

## Review Focus

- Credential conflicts (`p` together with `t`/`s`) must fail rather than silently selecting a mode — pinned in Subsonic middleware table tests.
- Disabled users must fail through every authentication path, including an already-issued session — pinned in API and Subsonic tests.
- Expired or revoked sessions must fail after logout and after restart — pinned in session lifecycle integration tests.
- Passwords, tokens, salts, and api keys must not appear in logs or returned errors — pinned in redaction tests.
- GET and POST credential parameter handling must agree where the endpoint permits both — pinned in protocol request tests.

---

### Task 1: Add explicit user disablement and shared session lifecycle checks

**Files:**
- Modify: `backend/internal/database/models.go` — add nullable `DisabledAt *time.Time` to `database.User`.
- Modify: `backend/internal/database/migrate.go` or the existing migration entry point — ensure the additive column is handled by runtime migration.
- Create: `backend/internal/database/user_disablement_migration_test.go` — verify the column exists and preserves NULL/non-NULL values on PostgreSQL when the gate is available.
- Modify: `backend/internal/api/auth.go` — centralize session lookup and reject disabled users in `AuthMiddleware`, `OptionalAuthMiddleware`, and login.
- Modify: `backend/internal/api/auth_unit_test.go` — add disabled, expired, logout-revoked, and concurrent-session cases.

**Interfaces:**
- Produces a single session validation path used by browser/management middleware and login policy.
- Produces a nullable `users.disabled_at` field; NULL means enabled, non-NULL means authentication is refused.

- [ ] **Step 1: Write failing tests** named `TestAuthMiddleware_DisabledUserRejected`, `TestOptionalAuthMiddleware_DisabledUserNotLoaded`, `TestLogin_DisabledUserRejected`, and `TestLogout_RevokesOnlyPresentedSession`.
- [ ] **Step 2: Run the focused auth tests** with `env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test ./internal/api -run 'Test(AuthMiddleware|OptionalAuthMiddleware|Login|Logout)' -count=1`; expect failures for missing disablement behavior.
- [ ] **Step 3: Add `DisabledAt *time.Time` and migration support**, then implement one internal lookup that checks session existence, expiry, and user disablement before populating `c.Locals("user")`.
- [ ] **Step 4: Run the focused tests again** and require PASS with no browser redirect regressions.
- [ ] **Step 5: Add the PostgreSQL migration proof** following the repository’s `setupPostgresForMigration` skip/gate conventions; assert NULL and timestamp semantics.
- [ ] **Step 6: Run `go test ./internal/database ./internal/api -count=1`** with environment overrides cleared; require PASS.
- [ ] **Step 7: Commit** with `git add backend/internal/database backend/internal/api && git commit -m "feat(auth): enforce account disablement across sessions"`.

### Task 2: Complete the management authentication lifecycle contract

**Files:**
- Modify: `backend/internal/api/auth.go` — add `GET /api/auth/me` handler returning the authenticated principal and session expiry without password/session-token material.
- Modify: `backend/cmd/server/main.go` — register `/api/auth/me` under the existing session middleware and leave login/logout route behavior intact.
- Create: `backend/internal/api/auth_lifecycle_test.go` — table-driven JSON lifecycle tests.
- Modify: `docs/CLIENT_SERVER_CONTRACT.md` — document login, identity, logout/revocation, expiry, and the session-cookie boundary.
- Modify: `docs/api/client-contract.yaml` — add the `/api/auth/me` operation and response schemas.

**Interfaces:**
- `GET /api/auth/me` returns `200` with the authenticated user identity, role, and session expiry; it never returns password hashes or raw session IDs.
- An unauthenticated management request returns the existing management JSON error contract.
- `POST /api/auth/logout` revokes the presented session and clears the cookie; subsequent identity, management, media, and websocket requests using that session fail.

- [ ] **Step 1: Write failing tests** named `TestAuthMe_ReturnsPrincipalWithoutSecrets`, `TestAuthMe_RejectsMissingSession`, `TestAuthLifecycle_LogoutRevokesSession`, `TestAuthLifecycle_ExpiredSessionRejected`, and `TestAuthLifecycle_ConcurrentSessionsRemainIndependent`.
- [ ] **Step 2: Run `go test ./internal/api -run 'TestAuth(Me|Lifecycle)' -count=1`** and verify the missing route/handler failures.
- [ ] **Step 3: Implement the minimal `AuthMe` handler and route registration** using the user already loaded by `AuthMiddleware`; expose only stable identity fields and `expires_at`.
- [ ] **Step 4: Run the lifecycle tests** and require PASS, including no `PasswordHash`, `SessionID`, or credential fields in serialized responses.
- [ ] **Step 5: Update the Markdown contract and OpenAPI YAML** with exact status codes, cookie behavior, revocation semantics, and no-device-token decision.
- [ ] **Step 6: Run `go test ./internal/api ./cmd/server -count=1`** with environment overrides cleared; require PASS.
- [ ] **Step 7: Commit** with `git add backend/internal/api backend/cmd/server docs/CLIENT_SERVER_CONTRACT.md docs/api/client-contract.yaml && git commit -m "feat(auth): expose management identity lifecycle"`.

### Task 3: Harden Subsonic credential mode selection and protocol redaction

**Files:**
- Modify: `backend/internal/api/subsonic.go` — make credential mode selection explicit; compare the computed and supplied token digests with `subtle.ConstantTimeCompare`; reject conflicts and malformed token/salt input without changing the Subsonic envelope.
- Create: `backend/internal/api/subsonic_auth_lifecycle_test.go` — table-driven protocol credential matrix.
- Modify: `backend/internal/api/subsonic_auth_hardening_test.go` — retain and extend the no-shared-password guard.
- Modify: `backend/internal/api/subsonic_test.go` — cover JSON/XML failure envelopes and GET/POST request shapes.
- Modify: `backend/internal/api/subsonic.go` — remove credential values from authentication log fields and keep only event type, username-independent failure reason, and protocol-safe context.
- Modify: `docs/CLIENT_SERVER_CONTRACT.md` and `docs/api/client-contract.yaml` — document supported `p` and `t`/`s` modes and explicitly document api-key status.

**Interfaces:**
- Subsonic middleware accepts exactly one supported credential mode: account password or configured shared token/salt.
- Invalid, missing, conflicting, disabled, and expired credentials return protocol code `40` inside the existing 200 envelope.
- Raw password, token, salt, and api-key values are never logged or returned.

- [ ] **Step 1: Write the table-driven matrix** `TestSubsonicAuth_CredentialModes` covering valid password, valid configured token, missing `u`, missing `p`, incomplete `t`/`s`, `p` plus `t`/`s`, invalid password, invalid token, disabled user, and unconfigured shared password.
- [ ] **Step 2: Add GET/POST request tests** named `TestSubsonicAuth_CredentialParameters_GetAndPost` for every endpoint/request shape that accepts POST parameters; assert identical principal/error behavior.
- [ ] **Step 3: Run `go test ./internal/api -run 'TestSubsonicAuth|TestSubsonic_AuthMiddleware' -count=1`** and verify the new conflict/redaction cases fail before implementation.
- [ ] **Step 4: Implement explicit mode selection** in `AuthMiddleware`: reject mixed modes, require both token and salt, refuse token mode without a configured shared secret, use constant-time comparisons, and preserve `respondError` protocol envelopes.
- [ ] **Step 5: Add log-capture assertions** proving credential values do not appear in authentication warnings or returned errors.
- [ ] **Step 6: Run the focused Subsonic tests** and require PASS for XML and JSON envelopes.
- [ ] **Step 7: Commit** with `git add backend/internal/api docs/CLIENT_SERVER_CONTRACT.md docs/api/client-contract.yaml && git commit -m "feat(subsonic): harden credential lifecycle"`.

### Task 4: Add lifecycle integration and compatibility evidence

**Files:**
- Modify: `backend/internal/integration/auth_smoke_test.go` — extend the real-server lifecycle with identity, expiry/revocation, and persistence checks after constructing a fresh handler against the same database.
- Create: `backend/internal/integration/subsonic_auth_smoke_test.go` — headless protocol auth matrix against the running app.
- Modify: `e2e/tests/auth.spec.ts` — add browser regression coverage for `/api/auth/me`, logout, and CSRF preservation.
- Modify: `docs/CLIENT_SERVER_CONTRACT.md` — record named/versioned client observations and explicit unresolved gaps.

**Interfaces:**
- Integration tests exercise the same HTTP shapes as third-party clients and do not bypass auth with injected locals.
- Browser tests continue to use the existing async CSRF fixture and do not assert protocol behavior through browser redirects.

- [ ] **Step 1: Write the failing integration lifecycle tests** for register → login → identity → protected request → logout → refusal, plus token/password protocol paths.
- [ ] **Step 2: Run the tagged integration tests against the running integration stack** with `INTEGRATION_TESTS=1 INTEGRATION_BASE_URL=http://localhost:18080 go test ./internal/integration/... -tags=integration -run 'Auth|Subsonic' -v`; record any environment-only skips separately.
- [ ] **Step 3: Implement only the missing production behavior exposed by those tests**; do not add test-only authentication paths.
- [ ] **Step 4: Run the focused integration tests again** and require PASS.
- [ ] **Step 5: Run the full E2E auth/browser subset** from `e2e/` with the stack rebuilt if backend code changed; require no new skips.
- [ ] **Step 6: Commit** with `git add backend/internal/integration e2e/tests docs/CLIENT_SERVER_CONTRACT.md && git commit -m "test(auth): prove client authentication lifecycles"`.

### Task 5: Full verification and NR03 completion evidence

**Files:**
- Modify: `docs/superpowers/specs/2026-10-15-nr03-authentication-lifecycle-design.md` only if implementation decisions require a precise correction.
- Modify: `docs/CLIENT_SERVER_CONTRACT.md` and `docs/api/client-contract.yaml` for final compatibility evidence.

- [ ] **Step 1: Run backend tests**: `env -u ENVIRONMENT -u CONFIG_ENV -u PORT go test ./...` from `backend/`; require all packages PASS.
- [ ] **Step 2: Run vet**: `env -u ENVIRONMENT -u CONFIG_ENV -u PORT go vet ./...`; require PASS.
- [ ] **Step 3: Run the repository Postgres gate** using the configured real Postgres environment; require every required test to execute rather than skip.
- [ ] **Step 4: Run full E2E**: `bash ../scripts/e2e.sh test` from `e2e/`; require every declared test to report a result and account for skips.
- [ ] **Step 5: Run `git diff --check` and inspect `git status`**; require a clean, intentional diff.
- [ ] **Step 6: Commit the final documentation/evidence changes**, then push `nr03-auth` and open the NR03 PR with the exact test evidence.
- [ ] **Step 7: Add the completion comment to DJI-563 and move it to Done only after the PR checks are green.**
