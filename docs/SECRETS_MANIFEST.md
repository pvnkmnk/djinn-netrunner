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
| `DATABASE_URL` (value `netrunner.db`, SQLite) | Infisical → `dev` | added 2026-10-09; the documented local single-binary dev value — compose overrides `DATABASE_URL` for containers anyway, so this only affects host-run processes |
| dev service token | `C:\Users\idols\DevWorks\.secrets\infisical-dev.token` (ACL: this user only) | `netrunner-dev-local`, scope `dev:/`, `--access-level read`, `--expiry-seconds 0`; created 2026-10-09 |

Not in place: `prod` is **empty**, and `gh secret list --repo pvnkmnk/djinn-netrunner` returns
**zero** repository secrets.

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
| prod service token | `st.`-prefixed string | the same two places, once `prod` exists |

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

## 3. Values still missing from Infisical

Destination for all of them: `prod` in the Infisical project (`dev` is complete as of
2026-10-09). Easiest `prod` fill: `infisical secrets set --env prod --file .env` from this
checkout, then fix the keys that must not be copied verbatim: `POSTGRES_PASSWORD` (and the
password inside `DATABASE_URL`) differ per environment, and `ENVIRONMENT`, `CONFIG_ENV`,
`APP_VERSION`, `DOMAIN`, `NAVIDROME_PORT`, `BETA_HTTP_PORT` are local-deploy values that
should not be inherited by `prod`.

| Key | Form / constraint |
| --- | --- |
| `POSTGRES_PASSWORD` | any strong string; must equal the password embedded in `DATABASE_URL` (URL-encoded) |
| `JWT_SECRET` | long random; production refuses to boot without it |
| `SLSKD_API_KEY` | **16–255 chars** — slskd logs its complaint and exits (code 0) below 16 |
| `SLSKD_USERNAME` / `SLSKD_PASSWORD` | real Soulseek account; searches and downloads fail without them |
| `BOOTSTRAP_ADMIN_SECRET` | **≥ 16 chars** (`config.MinBootstrapSecretLength`); without it `BOOTSTRAP_ADMIN_EMAIL` promotes nobody |
| `BOOTSTRAP_ADMIN_EMAIL` | an address only — safe to leave unset after first login |
| `SUBSONIC_PASSWORD` | required when `SUBSONIC_ENABLED=true` in production |
| `NAVIDROME_USER` / `NAVIDROME_PASS` | required in production when `NAVIDROME_URL` is set |
| `SPOTIFY_CLIENT_ID` / `SPOTIFY_CLIENT_SECRET` | Spotify app credentials, optional |
| `LASTFM_API_KEY`, `LISTENBRAINZ_TOKEN`, `DISCOGS_TOKEN`, `ACOUSTID_API_KEY`, `MUSICBRAINZ_API_KEY`, `LIDARR_API_KEY`, `PROXY_URL`, `YTDLP_PROXY`, SMTP keys | optional provider keys; an empty one degrades to a logged skip |

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
infisical secrets --env prod --plain | cut -d= -f1 | sort   # currently empty
gh secret list --repo pvnkmnk/djinn-netrunner               # expect the names from §2–§4
infisical run --project-config-dir "$REPO" --env dev -- go run ./backend/cmd/server
```
