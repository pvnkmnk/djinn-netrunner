# Security Remediation Plan

**Date:** 2026-03-23
**Status:** Approved
**Approach:** Sequential by Severity Within Theme

## Overview

Fix 18 security vulnerabilities in the netrunner codebase, organized by vulnerability class, starting with highest severity within each theme.

## Linear Issues

18 Backlog items tagged as security vulnerabilities.

---

## Theme 1: XSS Prevention

### DJI-142 (Critical) - Stored XSS in jobs.html
- **File:** `ops/web/templates/pages/jobs.html`
- **Issue:** Unescaped `CreatedBy` user-controlled data rendered in HTML
- **Fix:** Escape all user-provided fields before rendering in HTML templates

### DJI-155 (Low) - Potential XSS via innerHTML in JS
- **File:** `ops/web/static/js/app.js`
- **Issue:** Using `innerHTML` with user-controlled data
- **Fix:** Replace with `textContent` or sanitize input before DOM insertion

**Implementation:** Add template escaping utility, audit all templates for unescaped user data.

**Files to audit:** `ops/web/templates/**/*.html`, `ops/web/static/js/app.js`

---

## Theme 2: Command Injection

### DJI-231 / DJI-152 (High) - YtdlpService URL Sanitization
- **File:** `backend/internal/services/ytdlp.go` (assumed)
- **Issue:** Unsanitized URL parameter passed to `yt-dlp` command
- **Fix:** Validate URL format, block private IP ranges, whitelist allowed protocols

### DJI-151 (High) - TranscoderService FFmpeg Command Injection
- **File:** `backend/internal/services/transcoder.go` (assumed)
- **Issue:** User input used in FFmpeg command construction without sanitization
- **Fix:** Use safe command builder with argument arrays, validate file paths

**Implementation:** Add URL validation utility, implement safe command execution pattern.

**Files to verify:** Find YtdlpService and TranscoderService file locations

---

## Theme 3: Authentication & Sessions

### DJI-233 (High) - Session Cookie Configuration
- **File:** `backend/internal/api/auth.go`
- **Issue:** Missing `Secure` flag, empty `SameSite` attribute on session cookies
- **Fix:** Set `Secure=true`, `SameSite=Strict` or `SameSite=Lax`

### DJI-232 (High) - Missing CSRF Protection
- **File:** API handlers using HTMX
- **Issue:** State-changing endpoints lack CSRF token validation
- **Fix:** Implement CSRF middleware for HTMX endpoints, use double-submit cookie pattern

### DJI-140 (Low) - JWT Secret in Logs
- **File:** `backend/internal/config/config.go`
- **Issue:** JWT secret potentially logged in development mode
- **Fix:** Ensure secret values are redacted in log output

**Implementation:** Update cookie configuration, add CSRF middleware.

**Files to modify:** `backend/internal/api/auth.go`, `backend/internal/api/`

---

## Theme 4: Secrets & Configuration

### DJI-144 / DJI-153 (High/Medium) - Hardcoded JWT Secret
- **File:** `backend/internal/config/config.go`
- **Issue:** Weak default JWT secret hardcoded in source
- **Fix:** Require `JWT_SECRET` env var, fail startup if not set

### DJI-234 (Medium) - Gonic Default Credentials
- **File:** Gonic client configuration
- **Issue:** Hardcoded default credentials for Subsonic integration
- **Fix:** Move credentials to environment variables only

### DJI-138 (Medium) - Weak Docker Compose Defaults
- **File:** `docker-compose.yml`
- **Issue:** Weak default credentials for services
- **Fix:** Use strong generated passwords or require env var overrides

### DJI-149 (Medium) - Database SSL Disabled
- **File:** `docker-compose.yml`
- **Issue:** PostgreSQL connection lacks SSL/TLS
- **Fix:** Enable SSL mode for database connections

**Implementation:** Audit all secrets, enforce env var requirements.

**Files to audit:** `backend/internal/config/config.go`, `docker-compose.yml`, Gonic client config

---

## Theme 5: Information Disclosure

### DJI-236 (Medium) - Detailed API Error Messages
- **File:** API handlers
- **Issue:** Excessive error details exposed to clients
- **Fix:** Return generic errors to clients, log details server-side

### DJI-238 (Low) - User Enumeration via Registration
- **File:** `backend/internal/api/auth.go`
- **Issue:** Different error messages for existing vs new email on registration
- **Fix:** Return identical response for all registration failures

### DJI-237 (Low) - Missing Rate Limiting
- **File:** API handlers
- **Issue:** No rate limiting on non-auth endpoints
- **Fix:** Implement Redis-based rate limiter middleware

### DJI-146 (Medium) - Missing CSP Headers
- **File:** HTTP server configuration
- **Issue:** No Content-Security-Policy headers
- **Fix:** Add CSP headers to all responses

### DJI-141 (Low) - Weak Bcrypt Cost
- **File:** `backend/internal/api/auth.go`
- **Issue:** Low bcrypt cost factor for password hashing
- **Fix:** Increase cost to 12+ (current OWASP recommendation)

**Implementation:** Standardize error responses, add rate limiting infrastructure.

**Files to audit:** All API handlers, server configuration

---

## Execution Order

1. **XSS Prevention** - Critical first (DJI-142)
2. **Command Injection** - High priority services
3. **Authentication & Sessions** - Cookie and CSRF fixes
4. **Secrets & Configuration** - Remove hardcoded secrets
5. **Information Disclosure** - Error handling and rate limiting

## Verification Strategy

For each fix:
1. Write unit test that verifies the vulnerability is mitigated
2. Run security-focused test suite (auth tests, BOLA tests)
3. Manual testing with known exploit payloads
4. Consider using static analysis tools (e.g., `gosec`)

### Quick Wins (1-2 hours)
- DJI-142: XSS in jobs.html (template fix)
- DJI-140: JWT secret in logs (add redaction)
- DJI-238: User enumeration (return same error)

### Medium Effort (half-day)
- DJI-233: Cookie config (one file)
- DJI-141: Bcrypt cost (one constant)
- DJI-146: CSP headers (middleware)

### Complex (full day+)
- DJI-232: CSRF for HTMX (design + implementation)
- DJI-231/151: Command injection (sanitization library)
- DJI-237: Rate limiting (Redis infrastructure)

## Discovery Phase (First Step)

Before implementing, verify actual file locations for each Linear issue:

**Linux/Mac:**
```bash
find backend -name "*.go" | xargs grep -l "YtdlpService\|yt-dlp"
find backend -name "*.go" | xargs grep -l "TranscoderService\|ffmpeg"
grep -rn "secret\|password\|credential" backend/internal/config/
grep -n "innerHTML" ops/web/static/js/*.js
```

**Windows (PowerShell):**
```powershell
Get-ChildItem -Path backend -Recurse -Filter "*.go" | Select-String "YtdlpService|yt-dlp" | Select-Object -First 10
Get-ChildItem -Path backend -Recurse -Filter "*.go" | Select-String "TranscoderService|ffmpeg" | Select-Object -First 10
Select-String -Path "backend/internal/config/*.go" -Pattern "secret|password|credential"
Select-String -Path "ops/web/static/js/*.js" -Pattern "innerHTML"
```

## References

- OWASP Top 10
- Linear Issue IDs: DJI-142, DJI-155, DJI-231, DJI-152, DJI-151, DJI-233, DJI-232, DJI-140, DJI-144, DJI-153, DJI-234, DJI-138, DJI-149, DJI-236, DJI-238, DJI-237, DJI-146, DJI-141

## Branch Strategy

Create one branch per theme to keep PRs focused and reviewable:

```
fix/security-1-xss          # Theme 1: XSS Prevention
fix/security-2-injection    # Theme 2: Command Injection
fix/security-3-auth         # Theme 3: Authentication & Sessions
fix/security-4-secrets      # Theme 4: Secrets & Configuration
fix/security-5-disclosure   # Theme 5: Information Disclosure
```

Each branch:
1. Branch from `master`
2. Implement fixes for theme
3. Run tests, add security-specific tests
4. Update Linear issues to "In Review"
5. Create PR, merge to master
6. Mark Linear issues as "Done" after merge

## Linear Workflow

1. Before starting theme: Mark all issues as "In Progress"
2. After PR merge: Mark as "Done", add PR link as comment
3. Blocked issues: Add comment with dependencies