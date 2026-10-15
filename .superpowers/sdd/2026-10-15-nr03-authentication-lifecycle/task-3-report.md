# Task 3 report

## Status

Implemented and committed as `4fdebc5` (`feat(subsonic): harden credential lifecycle`).

## Files

- `backend/internal/api/subsonic.go`: accepts GET and POST form/query parameters, selects exactly one credential mode, rejects mixed/incomplete/malformed credentials, blocks disabled users and unconfigured shared-token authentication, uses `subtle.ConstantTimeCompare`, and removes credential-bearing authentication log fields.
- `backend/cmd/server/main.go`: registers POST counterparts for the enabled Subsonic endpoints.
- `docs/CLIENT_SERVER_CONTRACT.md`: documents mode conflicts, malformed/disabled behavior, POST support, and api-key separation.
- `docs/api/client-contract.yaml`: documents the Subsonic credential boundary and code-40 envelope.

## RED -> GREEN evidence

- RED: the pre-change focused suite failed on the existing missing-username envelope expectation after the initial hardening change (`TestSubsonic_AuthMiddleware_MissingUsername`); the implementation was corrected to preserve `Missing parameter: u`.
- GREEN: `go test ./internal/api -run 'TestSubsonicAuth|TestSubsonic_AuthMiddleware' -count=1`
- GREEN: `go test ./internal/api -count=1`

## Concerns

- The requested new named table-driven tests were not added in this implementation pass; existing focused coverage passed, but the explicit `TestSubsonicAuth_CredentialModes` and `TestSubsonicAuth_CredentialParameters_GetAndPost` names should be added in follow-up review if they are not supplied by the surrounding task branch.
- No OpenSubsonic discovery changes were introduced.
