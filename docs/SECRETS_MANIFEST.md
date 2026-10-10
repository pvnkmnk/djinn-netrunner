# Secrets manifest — where each remaining credential goes

Verified 2026-10-09 against Infisical project `2917e5c0-6a32-4828-96f5-bf779dac2f10`
(environments `dev` / `prod`), this checkout, and GitHub repo `pvnkmnk/djinn-netrunner`.
**No secret values appear in this file.**

`REPO` = `C:\Users\idols\DevWorks\projects\djinn-netrunner`, `HOME` = `C:\Users\idols`.
Every host-level credential below lands in `REPO\.env` because that is the one file both
`docker-compose.yml` and `docker-compose.release.yml` read (`env_file:`), and it is already
git-ignored.

## 1. Already in place

| What | Location | How it was verified |
| --- | --- | --- |
| Infisical project link | `REPO\.infisical.json` (untracked) | holds only the project id — no secret |
| App env vars, `dev` | Infisical → `dev` (38 names) | `infisical secrets --env dev --plain` names == the 36 keys in `REPO\.env` + `LINEAR_API_KEY` + `DATABASE_URL` |
| Linear CLI token | `HOME\.linear_token` (0600) | file present |
| Linear drain token | `HOME\.linear_drain_token` (0600) | file present |
| `DATABASE_URL` (host Postgres URL) | Infisical → `dev` | added 2026-10-09 as the SQLite `netrunner.db`, then repointed the same day to `postgresql://musicops:…@127.0.0.1:5432/musicops?sslmode=disable` (ADR 0004 / DJI-649) — the host form of the value both compose files inject for containers, so this key only affects host-run processes. A host-run server boots on it with `database: ok` and creates no `netrunner.db` |
| dev service token | `C:\Users\idols\DevWorks\.secrets\infisical-dev.token` (ACL: this user only) | `netrunner-dev-local`, scope `dev:/`, `--access-level read`, `--expiry-seconds 0`; created 2026-10-09 |
| app env vars, `prod` | Infisical → `prod` (24 names) | filled 2026-10-09 from the `dev` set with the per-environment overrides in §3; 22 of the 24 values are byte-identical to `dev`, only `DATABASE_URL` and `APP_VERSION` deliberately differ |

Still not in place: `gh secret list --repo pvnkmnk/djinn-netrunner` returns **zero**
repository secrets (§2, §4), and no `prod` service token exists — the `prod` boot proof in §3
used this CLI's interactive session, so only a human-driven `infisical run` works there today.

## 2. Infisical rollout — one credential per target

Pick one option and use it for every non-local target (Docker/self-hosted, GitHub Actions).

### Option A — service token (creatable from this CLI) — **dev token already created 2026-10-09**

```bash
# dev, never expires, read-only, scoped to the whole dev environment
infisical service-token create --name netrunner-docker-dev \
  --scope dev:/ --expiry-seconds 0 --access-level read --token-only
```

| Credential | Form | Exact destination |
| --- | --- | --- |
| dev service token, Docker target | `st.`-prefixed string, printed once by the command | `REPO\.env` → new line `INFISICAL_TOKEN=…` (both containers already receive it via `env_file: .env`) |
| dev service token, CI target | `st.`-prefixed string | GitHub repo secret `INFISICAL_TOKEN` (repo → Settings → Secrets and variables → Actions) |
| prod service token | `st.`-prefixed string | the same two places — **not created yet**; §3 records what `prod` holds without one |

> `--expiry-seconds` defaults to **86400 (1 day)**: a token created without
> `--expiry-seconds 0` stops working tomorrow, which is the failure this line exists to
> prevent. The scope is also mandatory — `--scope dev:/` or an unscoped token grants nothing.

### Option B — machine identity + Universal Auth (what the migration procedure names)

Created in the Infisical dashboard under **Access control → Machine identities → Universal
Auth**. CLI 0.43.140 cannot script this half (`infisical identities` →
`Error: unknown command "identities"`).

| Credential | Form | Exact destination |
| --- | --- | --- |
| Client ID (Docker target) | UUID `8-4-4-4-12` | `REPO\.env` → `INFISICAL_UNIVERSAL_AUTH_CLIENT_ID=…` |
| Client Secret (Docker target) | opaque string, shown once at creation | `REPO\.env` → `INFISICAL_UNIVERSAL_AUTH_CLIENT_SECRET=…` |
| Client ID (CI target) | UUID | GitHub repo secret `INFISICAL_UNIVERSAL_AUTH_CLIENT_ID` |
| Client Secret (CI target) | opaque string | GitHub repo secret `INFISICAL_UNIVERSAL_AUTH_CLIENT_SECRET` |
| Project id (CI) | UUID `2917e5c0-6a32-4828-96f5-bf779dac2f10` | GitHub repo secret `INFISICAL_PROJECT_ID` — required with machine-identity auth (`infisical run --projectId`) |

The variable names above are the ones this CLI build actually reads (0.43.140, string-verified
against the installed binary): `INFISICAL_UNIVERSAL_AUTH_CLIENT_ID`,
`INFISICAL_UNIVERSAL_AUTH_CLIENT_SECRET`, `INFISICAL_UNIVERSAL_AUTH_ACCESS_TOKEN`,
`INFISICAL_TOKEN`, `INFISICAL_PROJECT_ID`, `INFISICAL_ENVIRONMENT`, `INFISICAL_DOMAIN`.

## 3. Filling `prod` — what landed, and what differs

`infisical secrets set --env prod --file .env`, the obvious first move, **writes nothing**: a
single empty-valued key aborts the whole file (`Secret key 'SMTP_USER' has an empty value`),
and `REPO\.env` has 13 of them. What works is to copy the `dev` set, drop the empties, then
set the overrides:

```bash
SEED=/c/Users/idols/DevWorks/.secrets/prod-seed.env   # outside the checkout: .art/ is NOT ignored
infisical export --env dev --format=dotenv --expand=false --output-file "$SEED"
# an empty value exports as KEY="" — a bare '=$' filter keeps it
rg -v '^[A-Za-z_][A-Za-z0-9_]*=(""|[[:space:]])*$' "$SEED" > "$SEED.clean"
rg -v '^LINEAR_API_KEY=' "$SEED.clean" > "$SEED"
infisical secrets set --env prod --file "$SEED"
infisical secrets set --env prod DATABASE_URL=netrunner.prod.db APP_VERSION=v0.1.1 \
  ENVIRONMENT=production CONFIG_ENV=production
rm -f "$SEED" "$SEED.clean"                           # a plaintext dump of dev: delete it
```

`--expand=false` matters as much as the filter: with the default `--expand`, a `$` inside a
value (a password, a secret) is read as a shell expansion and the copy lands corrupted.

`prod` holds **24 names** = the 22 non-empty `dev` keys (minus `LINEAR_API_KEY`) +
`DATABASE_URL` + `APP_VERSION`. Compared against `dev` afterwards, 22 of the 24 values are
identical and exactly 2 differ:

| Key | `dev` | `prod` | Why |
| --- | --- | --- | --- |
| `DATABASE_URL` | `netrunner.db` | `netrunner.prod.db` | keeps a host-run `prod` off the `dev` database; containers never read this key — both compose files set it to the postgres URL explicitly |
| `APP_VERSION` | `dev` | `v0.1.1` | release pointer: `scripts/deploy.sh --release` resolves the latest git tag into this key |
| `ENVIRONMENT` / `CONFIG_ENV` | `production` | `production` | set explicitly in `prod`; note `dev` inherits `production` from `REPO\.env`, so a host-run "dev" process loads `config.production.yaml` and enforces production requirements |

`LINEAR_API_KEY` is deliberately absent from `prod`: both app services load `env_file: .env`,
the same reason §5 forbids Linear credentials in that file.

**Boot proof against `prod`** (2026-10-09, repo-root cwd, `PORT=18099`, `REPO\.env` present and
untouched): `infisical run --env prod -- <server>` returned

```
health_http=200
{"status":"ok","checks":{"database":{"status":"ok"},"slskd":{"status":"error","error":"service unreachable"}}}
INFO Loaded config overlay file=…\config.yaml
INFO Loaded config overlay file=…\config.production.yaml
netrunner.prod.db  netrunner.prod.db-shm  netrunner.prod.db-wal      # created
```

The database file name is the proof that the `prod` set was in effect: `REPO\.env` contains no
`DATABASE_URL` at all, so that value can only have come from Infisical. Production mode also
refuses to load without `JWT_SECRET`, so a 200 means the set supplied a usable one.
`slskd: "error"` is expected — nothing is published on host:5030. Afterwards: port 18099 free,
no stray process, no `netrunner.prod.db*` left in the checkout.

**Still open there** — each of these is wrong today or absent, and only the operator can
supply the real value:

| Key | Status today | Constraint / note |
| --- | --- | --- |
| `POSTGRES_PASSWORD` | **copied from `dev`** | any strong string; must equal the password embedded in `DATABASE_URL` (URL-encoded). A fresh postgres volume initialises with this value |
| `JWT_SECRET` | **copied from `dev`** | long random; production refuses to boot without it. Sharing it across environments means a session cookie minted by `dev` validates against `prod` |
| `DATABASE_URL` | `netrunner.prod.db` | a host-run placeholder, and now the odd one out: `dev` moved to PostgreSQL on 2026-10-09 (ADR 0004 / DJI-649) while this still names a SQLite file. The deployed target is the postgres URL both compose files inject |
| `BOOTSTRAP_ADMIN_SECRET` | absent in both | **≥ 16 chars** (`config.MinBootstrapSecretLength`); without it `BOOTSTRAP_ADMIN_EMAIL` promotes nobody |
| `BOOTSTRAP_ADMIN_EMAIL` | absent in both | an address only — safe to leave unset after first login |
| `SLSKD_API_KEY` | copied from `dev` | **16–255 chars** — slskd logs its complaint and exits (code 0) below 16 |
| `SLSKD_USERNAME` / `SLSKD_PASSWORD` | copied from `dev` | real Soulseek account; searches and downloads fail without them |
| `SUBSONIC_PASSWORD` | copied from `dev` | required when `SUBSONIC_ENABLED=true` in production |
| prod service token | not created | §2 — needed for the Docker and CI targets |
| GitHub repo secrets | zero | §2, §4 |

The optional provider keys (`NAVIDROME_URL`, `NAVIDROME_USER` / `NAVIDROME_PASS`,
`SPOTIFY_CLIENT_ID` / `SPOTIFY_CLIENT_SECRET`, `LASTFM_API_KEY`, `LISTENBRAINZ_TOKEN`,
`DISCOGS_TOKEN`, `ACOUSTID_API_KEY`, `MUSICBRAINZ_API_KEY`, `LIDARR_*`, `SMTP_*`, `PROXY_URL`,
`NOTIFICATION_WEBHOOK_URL`) are deliberately absent from `prod`: an empty value degrades to a
logged skip, so fill each one there only when that integration is actually used in `prod`.
An empty `SLSKD_API_KEY`/`SUBSONIC_PASSWORD` is *not* in that class — both are required.

## 4. Other credentials this repo's tooling and CI read

| Credential | Form | Exact destination |
| --- | --- | --- |
| `DEEPSEEK_API_KEY` | `sk-…` | GitHub repo secret `DEEPSEEK_API_KEY` |
| `ANTHROPIC_API_KEY` | `sk-ant-api03-…` | GitHub repo secret `ANTHROPIC_API_KEY` |
| `OPENAI_API_KEY` | `sk-proj-…` | GitHub repo secret `OPENAI_API_KEY` |
| `LINEAR_WEBHOOK_SECRET` | `lin_wh_…` (Linear generates it) | Cloudflare Worker secret: `cd ops/linear-webhook && npx wrangler secret put LINEAR_WEBHOOK_SECRET` |
| `DRAIN_TOKEN` | random, e.g. `python -c "import secrets;print(secrets.token_urlsafe(32))"` | Cloudflare Worker secret `DRAIN_TOKEN`, plus `HOME\.linear_drain_token` (0600) if this machine drains the queue |
| `GITHUB_TOKEN` | — | supplied by Actions automatically; nothing to place |

`.github/workflows/pr-sentry.yml` names the three provider keys and **skips** the job when all
three are empty — which is the current state, since the repo has no secrets. Alternatively
store these in Infisical and inject them into workflows with `infisical run` under
`INFISICAL_TOKEN`.

## 5. Handing a value over without pasting it into chat

Write it to a `0600` file outside the checkout, then name the path:

```bash
mkdir -p /c/Users/idols/DevWorks/.secrets     # once
# write the value there (editor, or a shell that does not echo it), then:
icacls 'C:\Users\idols\DevWorks\.secrets\<name>' /inheritance:r /grant:r "$USERNAME:(R,W)"
```

Never place Linear credentials in this repo's `.env`: both app services load that file through
`env_file:`, so the value would be injected into the web and worker containers.

## 6. Verify what landed (names only, never values)

```bash
infisical secrets --env dev  --plain | cut -d= -f1 | sort   # expect 38 names (incl. DATABASE_URL)
infisical secrets --env prod --plain | cut -d= -f1 | sort   # expect 24 names (no empty values)
gh secret list --repo pvnkmnk/djinn-netrunner               # expect the names from §2–§4
infisical run --project-config-dir "$REPO" --env dev -- go run ./backend/cmd/server
```
