# Security Remediation Plan

**Date:** 2026-03-23
**Status:** Approved (Discovery Complete)
**Approach:** Sequential by Severity Within Theme

## Overview

Fix 18 security vulnerabilities in the netrunner codebase, organized by vulnerability class, starting with highest severity within each theme.

## Discovery Results

| Theme | Linear Issue | File Location | Status |
|-------|--------------|---------------|--------|
| XSS | DJI-142 | `ops/web/templates/partials/jobs.html` (line 28) | Needs fix |
| XSS | DJI-155 | `ops/web/static/js/app.js` (lines 9,16,26,141) | Needs fix |
| Injection | DJI-231/152 | `backend/internal/services/ytdlp_service.go` | Partial fix (needs SSRF) |
| Injection | DJI-151 | `backend/internal/services/transcoder_service.go` | Already mitigated |
| Auth | DJI-233 | `backend/internal/api/auth.go` | Needs fix |
| Auth | DJI-232 | HTMX endpoints | Needs design |
| Auth | DJI-140 | `backend/internal/config/config.go` | Review logs |
| Secrets | DJI-144/153 | `backend/internal/config/config.go` | Already uses random gen |
| Secrets | DJI-234 | Gonic client config | Needs env var only |
| Secrets | DJI-138 | `docker-compose.yml` | Needs audit |
| Secrets | DJI-149 | `docker-compose.yml` | Needs SSL config |
| Disclosure | DJI-236 | API handlers | Needs generic errors |
| Disclosure | DJI-238 | `backend/internal/api/auth.go` | Needs same error |
| Disclosure | DJI-237 | API handlers | Needs rate limit |
| Disclosure | DJI-146 | HTTP server config | Needs CSP headers |
| Disclosure | DJI-141 | `backend/internal/api/auth.go` | Needs bcrypt cost 12+ |

---

## Theme 1: XSS Prevention (2 issues)

### DJI-142 (Critical) - Stored XSS in jobs.html
- **File:** `ops/web/templates/partials/jobs.html`
- **Line 28:** `<span>By: {{ job.CreatedBy }}</span>` - unescaped user data
- **Fix:** Use template escaping (Pongo2 auto-escapes, verify `{{ }}` not raw)

### DJI-155 (Low) - Potential XSS via innerHTML in JS
- **File:** `ops/web/static/js/app.js`
- **Lines:** 9, 16, 26, 141 - innerHTML usages
- **Fix:** Replace with `textContent` where possible, use DOMParser for untrusted HTML

**Quick fix:** Pongo2 templates auto-escape by default. Check if `CreatedBy` needs manual escape.

---

## Theme 2: Command Injection (2 issues)

### DJI-231 / DJI-152 (High) - YtdlpService URL Sanitization
- **File:** `backend/internal/services/ytdlp_service.go`
- **Status:** ✅ Already has URL validation (lines 29-37)
- **Missing:** SSRF protection (block private IPs like 10.x, 192.168.x, localhost)
- **Fix:** Add private IP range blocking using `net.ParseCIDR` or similar

### DJI-151 (High) - TranscoderService FFmpeg Command Injection
- **File:** `backend/internal/services/transcoder_service.go`
- **Status:** ✅ Already mitigated with `filepath.Rel` path validation (lines 40-49)
- **Fix:** Verify validation is sufficient for all edge cases

---

## Theme 3: Authentication & Sessions (3 issues)

### DJI-233 (High) - Session Cookie Configuration
- **File:** `backend/internal/api/auth.go`
- **Issue:** Missing `Secure` flag, empty `SameSite` attribute
- **Fix:** Set `Secure=true`, `SameSite=Strict` in cookie options

### DJI-232 (High) - Missing CSRF Protection
- **File:** HTMX endpoints
- **Issue:** State-changing endpoints lack CSRF token validation
- **Fix:** Implement CSRF middleware for HTMX (double-submit cookie pattern)

### DJI-140 (Low) - JWT Secret in Logs
- **File:** `backend/internal/config/config.go`
- **Status:** ✅ Auto-generates random secret (lines 97-102)
- **Issue:** Warning log message - verify no secret value exposed
- **Fix:** Review log output, ensure only generic message shown

---

## Theme 4: Secrets & Configuration (4 issues)

### DJI-144 / DJI-153 (High/Medium) - Hardcoded JWT Secret
- **File:** `backend/internal/config/config.go`
- **Status:** ✅ Already generates random secret if not set (lines 97-102)
- **Fix:** May want to fail startup in production instead of auto-generating

### DJI-234 (Medium) - Gonic Default Credentials
- **File:** `backend/internal/services/gonic_client.go`
- **Status:** ✅ Requires env vars (no hardcoded defaults in config.go)
- **Fix:** Verify gonic_client.go doesn't have hardcoded fallbacks

### DJI-138 (Medium) - Weak Docker Compose Defaults
- **File:** `docker-compose.yml`
- **Fix:** Audit for weak default passwords, use strong generated values

### DJI-149 (Medium) - Database SSL Disabled
- **File:** `docker-compose.yml`
- **Fix:** Enable SSL mode for PostgreSQL connections

---

## Theme 5: Information Disclosure (5 issues)

### DJI-236 (Medium) - Detailed API Error Messages
- **File:** All API handlers
- **Fix:** Return generic errors to clients, log details server-side

### DJI-238 (Low) - User Enumeration via Registration
- **File:** `backend/internal/api/auth.go`
- **Fix:** Return identical error message for all registration failures

### DJI-237 (Low) - Missing Rate Limiting
- **File:** API handlers
- **Fix:** Implement Redis-based rate limiter middleware

### DJI-146 (Medium) - Missing CSP Headers
- **File:** HTTP server configuration
- **Fix:** Add Content-Security-Policy headers

### DJI-141 (Low) - Weak Bcrypt Cost
- **File:** `backend/internal/api/auth.go`
- **Fix:** Increase bcrypt cost from default to 12+

---

## Execution Order

1. **DJI-142** (Critical): XSS in jobs.html - Quick template fix
2. **DJI-155** (Low): innerHTML in JS - Review and fix
3. **DJI-233** (High): Cookie config - One-file change
4. **DJI-238** (Low): User enumeration - One-file change
5. **DJI-141** (Low): Bcrypt cost - One constant change
6. **DJI-231** (High): SSRF protection - Add IP blocking
7. **DJI-232** (High): CSRF for HTMX - Design + implementation
8. **DJI-237** (Low): Rate limiting - Redis infrastructure
9. **DJI-146** (Medium): CSP headers - Middleware
10. **Remaining**: Review and fix docker-compose, error messages

---

## Quick Wins (< 2 hours)

| Issue | Fix | Files |
|-------|-----|-------|
| DJI-142 | Template escaping | `partials/jobs.html` |
| DJI-238 | Same error response | `api/auth.go` |
| DJI-141 | Bcrypt cost 12+ | `api/auth.go` |
| DJI-233 | Cookie Secure/SameSite | `api/auth.go` |

---

## Verification Strategy

For each fix:
1. Write unit test that verifies vulnerability is mitigated
2. Run security-focused test suite (auth tests, BOLA tests)
3. Manual testing with known exploit payloads
4. Static analysis tools (gosec)

### Test Payloads

| Vulnerability | Payload |
|--------------|---------|
| XSS | `<script>alert(1)</script>` |
| Command Injection | `; rm -rf /` |
| URL Injection | `file:///etc/passwd` |
| SQL Injection | `' OR '1'='1` |

---

## Branch Strategy

```
fix/security-1-xss          # DJI-142, DJI-155
fix/security-2-injection    # DJI-231, DJI-152, DJI-151
fix/security-3-auth         # DJI-233, DJI-232, DJI-140
fix/security-4-secrets      # DJI-144, DJI-153, DJI-234, DJI-138, DJI-149
fix/security-5-disclosure   # DJI-236, DJI-238, DJI-237, DJI-146, DJI-141
```

---

## Linear Workflow

1. Before starting theme: Mark all issues as \"In Progress\"
2. After PR merge: Mark as \"Done\", add PR link as comment
3. Blocked issues: Add comment with dependencies

---

## References

- OWASP Top 10
- Linear Issue IDs: DJI-142, DJI-155, DJI-231, DJI-152, DJI-151, DJI-233, DJI-232, DJI-140, DJI-144, DJI-153, DJI-234, DJI-138, DJI-149, DJI-236, DJI-238, DJI-237, DJI-146, DJI-141