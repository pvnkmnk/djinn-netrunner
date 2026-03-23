# backend/cmd/cli/

## Responsibility
Cobra-based CLI providing human and agent-friendly command-line access to all NETRUNNER operations.

## Design
- Root command `netrunner-cli` with `--json` flag for machine-readable output
- `PersistentPreRun` loads config and connects DB before any subcommand
- Subcommands: `status`, `config`, `watchlist`, `library`, `profile`, `stats`, `artist`, `job`, `search`
- Each subcommand is a factory function returning `*cobra.Command`
- Reuses `internal/agent` functions for core logic (shared with MCP server)

## Flow
1. `main()` → register `--json` flag → add subcommands → `rootCmd.Execute()`
2. `PersistentPreRun` → `config.Load()` → `database.Connect()` → set package globals
3. Subcommand → parse flags/args → call agent/service function → format output

## Integration
- **Consumes**: `internal/agent`, `internal/services`, `internal/database`, `internal/config`
- **Consumed by**: Terminal users, automation scripts, CI/CD
- **Shared surface**: Agent functions shared with `cmd/agent` (MCP server)
