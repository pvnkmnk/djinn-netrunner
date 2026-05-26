# ADR 0002: Config-as-Code (YAML/CLI Configuration)

## Status
Rejected — env vars remain the primary config mechanism.

## Context
NetRunner currently uses environment variables (loaded via `godotenv` from `.env`) for all configuration. With 40+ config fields spanning server, database, multiple service integrations, SMTP, proxy, and rate limiting, the question arises whether a `netrunner.yaml` config file would improve DX for complex deployments.

## Evaluation

### Current Approach (Env Vars)
**Strengths:**
- Zero-dependency: `os.Getenv` + `godotenv` for `.env` files
- Docker/Kubernetes native: env vars are the standard injection mechanism for containers
- Secret-safe: env vars integrate with Docker secrets, Kubernetes secrets, Vault, etc. without file mount complexity
- Simple override: `SLSKD_URL=http://other:5030 go run ./cmd/server` works instantly
- Well-documented: `.env.example` serves as both template and documentation

**Weaknesses:**
- Flat namespace: `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER` vs nested `smtp.host`, `smtp.port`
- No validation at parse time (fields silently default)
- No comments or grouping in env var names

### YAML Alternative
**Hypothetical `netrunner.yaml`:**
```yaml
server:
  port: 8080
  domain: netrunner.local
database:
  url: postgresql://user:pass@localhost:5432/netrunner
integrations:
  spotify:
    client_id: xxx
    client_secret: xxx
  slskd:
    url: http://localhost:5030
    api_key: xxx
  gonic:
    url: http://localhost:4747
    user: admin
    pass: secret
smtp:
  host: smtp.example.com
  port: 587
  user: notify@example.com
```

**Would gain:**
- Nested grouping for readability
- Inline comments
- Multi-environment file support (`netrunner.prod.yaml`)

**Would lose:**
- Docker secret injection requires file mounts or envsubst preprocessing
- Two config sources to reconcile (yaml + env overrides)
- Additional dependency (yaml parser)
- Breaking change for all existing deployments
- `.env.example` would need a parallel `netrunner.example.yaml`

### CLI Configuration
A `netrunner config set smtp.host smtp.example.com` CLI command could store config in the database `settings` table (which already exists). This is partially implemented via the MCP `update_config` tool, but is limited to runtime settings — not startup config like `DATABASE_URL`.

## Decision
**Keep environment variables as the sole config mechanism.** The current approach aligns with:
1. Docker/Kubernetes deployment conventions
2. The "appliance" vision (minimal config, sane defaults)
3. Existing documentation and `.env.example`
4. Secret management best practices

### Improvements to make instead:
1. **Startup validation**: `config.Load()` should validate all fields and report a consolidated error listing every invalid/missing value, not fail on the first one
2. **Config summary**: `netrunner-cli config list` already exists and shows non-sensitive config
3. **Groups in `.env.example`**: Already organized with comments — sufficient for discoverability

## Consequences
- No YAML config file will be introduced
- Future config additions continue as env vars in `config.go` + `.env.example`
- If nested config complexity grows significantly (e.g., per-library quality overrides), revisit this decision
