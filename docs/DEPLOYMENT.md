# Deployment (single machine)

Bring up NetRunner as a self-contained deployment on one Docker host: PostgreSQL,
slskd (Soulseek), the API/UI, the background worker, and optionally an external
Subsonic server. NetRunner serves its own Subsonic-compatible API, so a music
client can stream from it without any extra service.

Verified against `master` for the release-readiness work (see the Linear
project *NetRunner Beta Readiness*).

## Two canonical paths

NetRunner has exactly two ways to run, and they differ in the mode the app runs
in and in the version it reports:

| Path | Bring it up | Mode | Footer version |
|---|---|---|---|
| **dev** | `./scripts/deploy.sh` | development — base `docker-compose.yml` | `NetRunner vdev`, unless you set `APP_VERSION` |
| **release** | `./scripts/deploy.sh --release` | production — base + `docker-compose.release.yml` | the tag on HEAD, e.g. `NetRunner v0.1.1` |

Both publish the app on `127.0.0.1:${APP_HTTP_PORT:-8080}` over plain HTTP (see
the deployment boundary below), so run one at a time. `--profile edge` adds Caddy
for TLS on `:443`; `--profile media-server` adds the bundled Navidrome. One gate
covers both: `./scripts/smoke.sh --dev` or `./scripts/smoke.sh --release` (the
default).

## Prerequisites

- Docker Engine / Docker Desktop with Compose v2.24+ (`env_file.required` support)
- A Soulseek account for slskd (acquisition is inert without one)
- ~5 GB free for images plus room for the music library and downloads

Acoustic fingerprinting shells out to `fpcalc` (Chromaprint), which the image
installs and the build asserts is present — an image without it cannot be
built at all. Fingerprinting runs on every scanned and imported file, and each
scan reports how many files it fingerprinted. AcoustID enrichment is the one
optional step left: it needs `ACOUSTID_API_KEY`, and without a key the lookup is
not attempted and the track is recorded as **unscored** rather than as a zero.

`acoustid_score` is deliberately nullable. `NULL` means the lookup never produced
a measurement — no key, no fingerprint, the lookup failed, or AcoustID had no
match — and `0` means the lookup ran and returned that confidence. The two are
different facts, and before this was fixed the column was a plain `int`, so every
track in every deployment reported a score of zero that had never been measured.
Deployments upgrading through that version had their legacy zeros backfilled to
`NULL` on first start; a zero written after that is a real measurement and is
left alone.

## 1. Configure

```bash
git clone https://github.com/pvnkmnk/djinn-netrunner.git
cd djinn-netrunner
cp .env.release.example .env   # release path
# dev path instead:  cp .env.example .env
```

Change every `change_me_` value. The four that matter most:

| Variable | Why |
|---|---|
| `JWT_SECRET` | Session signing key. Without it in production the server **refuses to start** — an ephemeral secret would silently invalidate every login on restart. Generate one: `py -c "import secrets;print(secrets.token_urlsafe(48))"` |
| `SUBSONIC_PASSWORD` | Shared password for Subsonic token auth. Production refuses to start with Subsonic enabled and no password, because the token path would be `md5("" + salt)` — forgeable. |
| `SLSKD_USERNAME` / `SLSKD_PASSWORD` | Real Soulseek credentials; searches and downloads fail without them. |
| `SLSKD_API_KEY` | The key the app sends as `X-API-Key`, and the same value compose hands to slskd as its primary key. **At least 16 characters** — slskd logs `API key must be between 16 and 255 characters` and exits (code 0) below that. The stack wires both sides automatically; a hand-written compose file must set it on the slskd service too. |

The file is read twice: for `${VAR}` substitution inside the compose files, and
as the runtime environment of the `ops-web` / `ops-worker` containers (`env_file`
in `docker-compose.yml`). Values the compose files set explicitly — `DATABASE_URL`,
`SLSKD_URL`, `MUSIC_LIBRARY`, `DOWNLOAD_STAGING`, paths — win over `.env`.

### Supplying secrets from Infisical (optional)

`.env` is not the only source. The project's `dev` environment mirrors it in
Infisical (plus `DATABASE_URL`), and a process can take its environment from
there instead:

```bash
infisical login                        # once per machine
infisical run --project-config-dir . --env dev -- go run ./backend/cmd/server
```

The repo-root `.env` is not read by that process: `config.Load()` loads
`../../.env`, which resolves above the checkout, and that error is discarded.
So `infisical run` is the source of the environment, not a supplement to it.

- `dev` carries the **host** Postgres URL
  (`postgresql://musicops:…@127.0.0.1:5432/musicops?sslmode=disable`) because
  `.env` has no `DATABASE_URL` at all — compose injects the in-network one for the
  containers. It resolves against the dev stack's own database service:
  `docker compose up -d postgres` publishes `127.0.0.1:${PG_HOST_PORT:-5432}`.
  Keep the loopback **literal**: `localhost` resolves to `::1` here first while
  the publish is IPv4-only. `DATABASE_URL=netrunner.db` (SQLite, what
  `.env.example` documents) is the reduced-capability NetrunnerLite path — see
  `docs/decisions/0004-postgres-first-development-and-editions.md`.
- A headless target (CI, a self-hosted image) wants a service token:
  `infisical service-token create --name <name> --scope dev:/ --expiry-seconds 0
  --access-level read --token-only`, passed on as `INFISICAL_TOKEN`. The expiry
  defaults to **one day**, and a token without `--scope` grants nothing.
- `docs/SECRETS_MANIFEST.md` lists every credential the project needs, what
  form it takes, and the exact file (and key) it belongs in.

### Versioning the image

The page footer names the running version. That version is stamped into the
image at build time rather than hardcoded, and it is also the image **tag**:
compose builds `djinn-netrunner-ops-web:${APP_VERSION}` and
`djinn-netrunner-ops-worker:${APP_VERSION}`, so a dev build (`:dev`) and a
release (`:v0.1.1`) can never occupy the same tag. An undeclared bring-up
resolves to `:dev`, so it structurally cannot pick up the release image an
earlier run built — you do not need `--build` to be safe from that collision.

`scripts/deploy.sh` decides `APP_VERSION` for you, from the checked-out tag:

```bash
./scripts/deploy.sh --release        # base stack + release overlay
```

It resolves the version in this order — an explicit `APP_VERSION` (an override,
as a CI build of an untagged commit would set), then the tag pointing at HEAD
(`git describe --tags --exact-match`), then `dev` — writes the result back to
`.env` as `APP_VERSION=…`, and runs the compose bring-up. Check out the tag you
are releasing, run it once, and the tag you deploy is the version you build;
the declaration lives in `.env`, where `scripts/smoke.sh` reads it.

**`vdev` is a deliberate contract, not a defect.** A build that declares no
version reports `NetRunner vdev` — not a release number, so an un-stamped image
can never masquerade as a release it is not. Check 12 of `scripts/smoke.sh`
encodes exactly that expectation:

* a declared `APP_VERSION` must match the footer, or smoke fails with
  `footer says '…' but APP_VERSION is '…'`;
* no declared version must render as `NetRunner vdev`, or smoke fails with
  `no APP_VERSION declared but the footer claims '…'`.

So `NetRunner vdev` in the footer, and `[PASS] no APP_VERSION declared; the
image reports itself as dev` in the smoke output, both mean the mechanism is
working. You see them when you build by hand (`docker compose … up -d --build`
with nothing declared) rather than through `scripts/deploy.sh`, which always
declares a version.

### Deployment boundary

The app talks to slskd as `http://netrunner-slskd:5030` over the compose
network, sending `SLSKD_API_KEY` as `X-API-Key`. That traffic is cleartext, so
the Docker network **is** the trust boundary: only containers in this stack, on
a host you control, may join it. Do not attach untrusted workloads to it, and do
not publish slskd's port. If slskd ever moves off-host, or crosses a network you
do not control, enable TLS on slskd and set `SLSKD_URL` to `https://…` so the key
is not sent in the clear.

## 2. Bring it up

```bash
./scripts/deploy.sh --release
```

The script derives `APP_VERSION` from the checked-out tag and runs the
equivalent of:

```bash
docker compose -f docker-compose.yml -f docker-compose.release.yml up -d --build
```

The base file publishes the app on `${APP_HTTP_PORT:-8080}` and pins development
semantics; the overlay pins production, enables Subsonic, adds restart/log
policies, and runs the yt-dlp egress sidecar. Caddy is behind the `edge` profile
in both, so the stack serves plain HTTP instead of presenting a local-CA
certificate; add `--profile edge` when you want TLS on `:443`.

Optional external Subsonic server over the same music volume:

```bash
./scripts/deploy.sh --release --profile media-server
# then set NAVIDROME_URL=http://navidrome:4533 in .env and re-run -- the build is cached
```

## 3. Verify

The fastest health check is the smoke script. It asserts the container
configuration, Subsonic auth (both schemes), session persistence across a
restart, library scanning, Subsonic visibility and real audio bytes, and exits
non-zero listing every failing check:

```bash
./scripts/smoke.sh
# ...
# NetRunner smoke: all checks passed.
```

Add `--keep` to leave the throwaway library and audio fixtures in place for
inspection. Point it at another host with `SMOKE_BASE_URL=https://music.example`.

For a manual pass, start with the health endpoint:

```bash
curl -s http://localhost:8080/api/health
# {"status":"ok","checks":{"database":{"status":"ok"},"disk":{"status":"ok",...},"slskd":{"status":"ok"}}}
```

Confirm the environment actually reached the container — this was the class of
bug that made earlier deployments behave inexplicably:

```bash
docker compose -f docker-compose.yml -f docker-compose.release.yml exec ops-web env | grep -E '^(JWT_SECRET|SUBSONIC_ENABLED|CONFIG_ENV)='
```

### Become the first admin

Registration creates every account as a plain user, and nothing else in the
stack promotes anyone. So before registering, put the account you want to
administer NetRunner into `.env`:

```bash
BOOTSTRAP_ADMIN_EMAIL=you@example.com
BOOTSTRAP_ADMIN_SECRET=<a long random string you keep>
```

An address on its own promotes nobody. Promoting on one would give `admin` to
whoever registered it first, which needs no proof of control of that mailbox and
no knowledge beyond an address that is usually published. Both values are
required: the secret is your proof that the promotion is meant for that person,
so generate one (at least 16 characters) and hand it over out of band.
Production refuses to start with the address set and the secret missing.

That address is promoted to `admin` **once**, when it registers with the
enrollment code. The secret is checked where a code can actually be presented,
so registration is the trigger: an account that never presented one is never
promoted, not by registering and not by restarting.

A boot can only finish a promotion a registration already began, because a boot
has no code to compare. It promotes an account at the configured address only
when that account has presented the code, which keeps the retry after a failed
promotion working while leaving an account that merely happens to sit at that
address alone. **Set both values before registering.**

```bash
docker compose -f docker-compose.yml -f docker-compose.release.yml logs ops-web | grep -i bootstrap
# Bootstrap admin promoted email=you@example.com result="promoted you@example.com to admin"
```

It is safe to leave the variable set: the promotion is recorded, so later
boots are no-ops and a role you change afterwards — including demoting this
account — is never reverted. Clear it and recreate `ops-web` once you are
signed in as admin anyway, so the address is not left lying around in a
plaintext config file.

One consequence to be deliberate about: an account that already existed when
you set these values cannot be promoted by configuration, because it never
presented a code and the whole point of the secret is that the address alone is
not enough. Promote it explicitly, then clear both values:

```sql
UPDATE users SET role = 'admin' WHERE email = 'you@example.com';
```

Registration itself stays open to whoever can reach the port. That is unchanged
by this variable, and it is no longer a route to `admin`. Treat the secret as a
password: hand it over out of band, and clear both values and recreate `ops-web`
once you are signed in, so neither is left in a plaintext config file.

### Account password policy

Every route that accepts a password enforces the same floor and the same
ceiling — `POST /api/auth/register`, `POST /api/admin/users`, and
`POST /api/admin/users/:id/reset-password` — and they are counted in
**different units**:

| Bound | Value | Counted in | Why |
|---|---|---|---|
| Floor | `MIN_PASSWORD_LENGTH`, default **12** | characters (runes) | the minimum a passphrase must have to be worth having |
| Ceiling | **72** | bytes | bcrypt's own limit — it is a hash function, not a string library, and it refuses more |

So a password must be **at least 12 characters and at most 72 bytes**. All three
routes check this on the server and return the identical `400` body, so a
password an admin sets through the API cannot be one registration would have
refused. The register form states both bounds; nothing about the ceiling is
expressible in HTML, because a browser's `minlength` counts characters and has
no byte notion at all.

The two units diverge outside ASCII, and that is the case worth knowing about:
40 `é` characters clear the 12-character floor easily and are **80 bytes**, so
they are refused even though no browser hint has told you anything is wrong.
Each route answers `400` naming the ceiling and your actual byte count, never
a `500` — bcrypt rejecting a password a person typed is a client error, not a
server fault. A passphrase with accented or non-Latin characters therefore
reaches the limit in *fewer* characters; the practical advice is to keep the
first one or two words of a passphrase in ASCII and let the rest be whatever.

The floor is configurable, but the ceiling is not: it is bcrypt's. Setting
`MIN_PASSWORD_LENGTH` above 72 makes the server refuse to start, because no
password could then satisfy both bounds.

Then create the first account. **Every state-changing request needs the CSRF
header**: any request (including `GET /`) sets a `csrf_` cookie, whose value must be echoed in
`X-CSRF-Token`.

The account password has to satisfy both ends of the policy, and the two are
counted in different units; *Account password policy* above has the exact bounds.

```bash
JAR=/tmp/nr.jar; rm -f $JAR
curl -s -b $JAR -c $JAR -o /dev/null http://localhost:8080/
TOKEN=$(awk '/csrf_/{print $7}' $JAR | tail -1)

curl -s -b $JAR -c $JAR -X POST http://localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $TOKEN" \
  -d '{"email":"you@example.com","password":"correct-horse-battery-staple"}'

curl -s -b $JAR -c $JAR -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $TOKEN" \
  -d '{"email":"you@example.com","password":"correct-horse-battery-staple"}'

Login is HTMX-first: it answers **302 with a `Set-Cookie`, not a JSON body**, so a 302 means
it worked — a missing session cookie is the failure. (The `+` in an email address is also a
space in a query string, so URL-encode it in Subsonic requests.)

curl -s -b $JAR -o /dev/null -w '%{http_code}\n' http://localhost:8080/api/watchlists   # 200
```

The bootstrap is what makes the admin surface answer at all — a plain user gets
`403` on it — so this is the check that the account is genuinely an admin:

```bash
curl -s -b $JAR -o /dev/null -w '%{http_code}\n' http://localhost:8080/api/admin/users   # 200
# or open /admin: users, audit log, runtime config
```

Sessions must survive a restart — this is what `JWT_SECRET` buys:

```bash
docker compose -f docker-compose.yml -f docker-compose.release.yml restart ops-web
curl -s -b $JAR -o /dev/null -w '%{http_code}\n' http://localhost:8080/api/watchlists   # still 200
```

Streaming, both authentication styles:

```bash
# account password (u = your NetRunner email)
curl -s 'http://localhost:8080/rest/ping.view?u=you@example.com&p=change-this&v=1.16.1&c=smoke'
# <subsonicResponse status="ok" version="1.16.1" type="netrunner"></subsonicResponse>

# shared-password token auth: t = md5(md5(SUBSONIC_PASSWORD) + salt)
```

## 4. Acquire your first artist

The checks above prove the stack is healthy. This walk-through proves it can
actually fill the library, following one artist all the way through acquire →
import → scan → Subsonic browse → stream. Every command below is safe to re-run.

State-changing requests need the CSRF header, and the `csrf_` cookie is minted by
*any* request — so if these start returning `403`, refresh both the jar and the
token:

```bash
curl -s -b $JAR -c $JAR -o /dev/null http://localhost:8080/
TOKEN=$(awk '/csrf_/{print $7}' $JAR | tail -1)
```

### Create the library

Acquisitions import into a library, and the library is what the scanner indexes.
`/app/music` is the shared music volume mounted into both `ops-web` and
`ops-worker`, so that path means the same thing on both sides:

```bash
LIBRARY_ID=$(curl -sS --fail-with-body -b $JAR -c $JAR -X POST http://localhost:8080/api/libraries -H 'Content-Type: application/json' -H "X-CSRF-Token: $TOKEN" -d '{"name":"Music","path":"/app/music"}' | tee /tmp/library.json | python -c "import sys,json;d=json.load(sys.stdin);print(d.get('id') or d.get('ID') or sys.exit('no library id in response: %r' % (d,)))")
echo "library: ${LIBRARY_ID:?no library id — see /tmp/library.json}"
```

`tee` keeps the response for you to read while the parser reads the same bytes
from the pipe. Three details stop a failed request from turning into a confusing
one: `--fail-with-body` makes an HTTP error non-zero while still keeping the
body for `tee`, the parser exits when neither key is present, and
`${VAR:?…}` refuses to continue with an empty id. Without them a `4xx` leaves
`LIBRARY_ID` holding the string `None`, and the next step quietly posts to
`/api/artists/None/sync`.

Note the two endpoints disagree on key casing — `/api/libraries`
answers `id`, `/api/artists` answers `ID` (Go's default field names) — so read
the key that is present rather than assuming one.

A path is unique across libraries, and re-running this is harmless: you get the
existing library back (`200`) when you own it, and `409` naming the existing row
when someone else does — never a bare `500`.

### Monitor an artist (its first scan is queued for you)

The `name` is resolved against MusicBrainz, which needs no API key:

```bash
ARTIST_ID=$(curl -sS --fail-with-body -b $JAR -c $JAR -X POST http://localhost:8080/api/artists -H 'Content-Type: application/json' -H "X-CSRF-Token: $TOKEN" -d '{"name":"PUP"}' | tee /tmp/artist.json | python -c "import sys,json;d=json.load(sys.stdin);print(d.get('id') or d.get('ID') or sys.exit('no artist id in response: %r' % (d,)))")
echo "artist: ${ARTIST_ID:?no artist id — see /tmp/artist.json}"
```

In the web UI the same call is the **Add Artist** modal on `/artists`. A name
MusicBrainz resolves to exactly one artist is created outright; an ambiguous one
comes back as a list of candidates, and clicking the row you meant is the step
that actually adds it — that click is what carries the MusicBrainz ID.

Nothing on that path needs script evaluation, which matters because the app
serves `script-src 'self'` with no `unsafe-eval`. The row used to build its
values in an htmx `js:` expression, which htmx compiles with `eval`: the click
threw a Content Security Policy error, issued no request at all, and left the
operator looking at a list that did nothing. Carry the values as ordinary
inputs collected by `hx-include` instead — do not loosen the header for it.
`e2e/tests/artist-picker.spec.ts` drives the whole path in a browser (search,
pick, artist created, scan queued), and `TestNoTemplateDependsOnEval` plus
`TestShippedCSPDoesNotOfferUnsafeEval` keep both halves of that true: no
template may need `eval`, and neither the server nor the proxy may offer it.

**Adding an artist queues its scan.** The row and the `artist_scan` job are
written in one transaction, so an artist that exists is an artist the worker has
been asked to look at — there is no state where a monitored artist is never
scanned. That scan pulls the discography, works out what is missing, and then
queues the `acquisition` job. Both are visible under `/jobs`.

The **Sync** button on each artist card is the same call, for when you want a
fresh look instead of the one Add already queued. Ask while a scan for that
artist is still queued or running and it hands that job straight back
(`sync_already_active`, with `HX-Trigger: sync-already-active`) rather than
queueing a second one — the worker takes an advisory lock keyed on
`scope_type:scope_id`, so two live scans of one artist would only serialise
behind each other:

```bash
curl -s -b $JAR -c $JAR -X POST "http://localhost:8080/api/artists/$ARTIST_ID/sync" -H 'Content-Type: application/json' -H "X-CSRF-Token: $TOKEN"
```

Watch the worker. It logs every stage, including each result it rejects and why —
peers that queue a transfer and never start sending, results that are not
plausible audio, and files that fail their audio probe:

```bash
docker compose -f docker-compose.yml -f docker-compose.release.yml logs -f ops-worker
```

A full discography is dozens of independent Soulseek searches and transfers, so
it takes a while and lands one track at a time. Cancel a running job with
`POST /api/jobs/:id/cancel`: the worker notices within one item and finishes the
job as `cancelled`, keeping whatever it had already imported.

### Scan, then browse and stream

The scanner is what turns files on disk into rows a Subsonic client can see, so
an import is invisible until a scan runs. One is queued automatically when an
acquisition finalizes — run against your media server if you configured one,
otherwise as a local scan of the library at `MUSIC_LIBRARY`. To force one:

```bash
curl -s -b $JAR -c $JAR -X POST "http://localhost:8080/api/libraries/$LIBRARY_ID/scan" -H 'Content-Type: application/json' -H "X-CSRF-Token: $TOKEN"
```

Wait for the scan job to finish, then walk artist → album → track and play the
last one. Note the `.view` suffix — see the troubleshooting table for why:

```bash
# artists, with stable resolvable ids
curl -s "http://localhost:8080/rest/getIndexes.view?u=you@example.com&p=change-this&v=1.16.1&c=netrunner"

# that artist and its albums
curl -s "http://localhost:8080/rest/getArtist.view?u=you@example.com&p=change-this&v=1.16.1&c=netrunner&id=artist-PUP"

# one track's audio
curl -s -o /tmp/track.m4a -w '%{http_code} %{size_download} bytes %{content_type}\n' "http://localhost:8080/rest/stream.view?u=you@example.com&p=change-this&v=1.16.1&c=netrunner&id=<track id>"
# 200 29087926 bytes audio/m4a
```

`stream.view` answering `200` with a non-zero byte count and an audio content
type is the point of the whole exercise: real audio, not an error document.

## 5. Streaming clients

| Field | Value |
|---|---|
| Server URL | `http://<host>:8080/rest` |
| Username | your NetRunner **email address** |
| Password | your account password (works with `p=` auth) |

If the client does not support token auth, the password field works as-is. If it
does, the shared `SUBSONIC_PASSWORD` is the password used for `t=`/`s=` token
auth; without a configured `SUBSONIC_PASSWORD` token requests are refused and
only the account password is accepted.

## 6. Operate

| Concern | Command |
|---|---|
| Logs | `docker compose -f docker-compose.yml -f docker-compose.release.yml logs -f ops-web ops-worker` |
| State | `docker volume ls \| grep netrunner` (postgres, downloads, music, config, logs, slskd) |
| Upgrade | `git fetch --tags && git checkout <tag> && ./scripts/deploy.sh --release`. Check out the release tag first — the script derives `APP_VERSION` from the tag on HEAD, so on an untagged branch commit it stamps `dev`, not the release. |
| Back up | see `ops/docs/backup.md` — back up the Postgres volume *and* the music volume together |
| Deduplicate a pre-existing library | `ops/docs/library-dedup-runbook.md` |
| Worker concurrency | `MAX_CONCURRENT_JOBS` in `.env` caps how many jobs one worker runs at once (default 5). Each running job holds peer connections and a download pipeline — raise only with the RAM to match. With SQLite the worker runs one at a time regardless (no advisory locks). Apply with the usual `up -d` to recreate the worker. |
| Reach it from another host | The app port binds to `127.0.0.1` because it serves plain HTTP with session cookies and Subsonic credentials. Put the `edge` profile's Caddy in front, or set `APP_BIND_ADDR=0.0.0.0` behind your own TLS terminator. `NAVIDROME_BIND_ADDR` works the same way for the optional media server. |
| Cancel a running job | `POST /api/jobs/:id/cancel` (CSRF header required). The worker aborts within one item and finishes the job as `cancelled`, keeping what it had already imported. |
| yt-dlp egress boundary | The overlay runs an `egress-proxy` sidecar (squid) that denies private/loopback ranges at connect time for every hop yt-dlp takes — including redirects the downloader follows on its own after handover (proven live: a source on a public address that 302s to an RFC1918 host is denied at the hop, `TCP_DENIED/403` in squid's log, and the item fails with the refusal in its log). The worker points yt-dlp at it via `YTDLP_PROXY`. **The allowlist is a feature, not a bug:** extraction sites live in `ops/squid/allowed-domains.txt`, and a host not on it is refused even when public — that is the boundary's default-deny posture, so add every site you acquire from. After editing it, apply with `docker compose -f docker-compose.yml -f docker-compose.release.yml restart egress-proxy` (a plain `up -d` leaves the running squid on its old in-memory allowlist). |
| Tear down (keep data) | `docker compose ... down` |
| Destroy (lose everything) | `docker compose ... down -v` |

## 7. Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Server exits, log says `JWT_SECRET is required in production` | `JWT_SECRET` empty or not injected — set it in `.env`. |
| Server exits, `SUBSONIC_PASSWORD is required in production when SUBSONIC_ENABLED=true` | Set `SUBSONIC_PASSWORD`, or set `SUBSONIC_ENABLED=false` if you do not want streaming. |
| `403` on register/login, or on a later request that worked before | Missing `X-CSRF-Token` header, or a stale token. `csrf_` is rotated by a login and re-minted by any request, so re-read it after logging in or restarting: `curl -s -b $JAR -c $JAR -o /dev/null http://localhost:8080/`. Both flags matter — with `-c` alone curl rewrites the jar without the session cookie. |
| Logins die after every restart | `JWT_SECRET` is not reaching the container: check `docker compose ... exec ops-web env \| grep JWT_SECRET`. |
| Every variable in `.env` seems ignored | The container predates the current compose file. `env_file` is applied at *create* time, so `docker compose restart` does not pick up `.env` changes — run `up -d` (add `--build` after a code change). A local override that replaces `env_file` has the same effect. `docker compose exec ops-web env` shows what actually arrived. |
| `exec /entrypoint.sh: no such file or directory` when building on Windows | The working copy has CRLF in `backend/entrypoint.sh`. The repo forces LF via `.gitattributes`; `git checkout -- backend/entrypoint.sh` (or `git config core.autocrlf input`) fixes it. |
| slskd exits immediately / downloads unwritable | `volume-init` must run before slskd; it chowns the shared volumes to UID 1000. Keep it in the stack. |
| `port is already allocated` | Another stack holds a published port. `APP_HTTP_PORT` covers the HTTP one, but the collision that usually blocks the whole bring-up is postgres: whatever already listens on 5432 (many hosts run a system or containerised postgres) fails the `up` before anything starts. Set `PG_HOST_PORT=15432` in `.env` — the stack itself reaches postgres over the compose network and ignores it, since the publish is for host-side debugging only. `NAVIDROME_PORT` does the same for the optional media server. Setting these beats stopping someone else's stack. |
| Music plays but nothing rescans in an external server | Set `NAVIDROME_URL` (+ user/pass) so the worker has a library client; `/api/health` then reports a `navidrome` check. |
| Every import logs `Audio fingerprinting failed: fpcalc failed: exec: "fpcalc": executable file not found in $PATH` | Not expected — the image installs Chromaprint and the build fails without it, so this means the container is not running the built image. Check `docker compose … exec ops-worker command -v fpcalc`; an empty answer means the container predates this change and needs `up -d --build`. |
| `Finished scan` reports `fingerprinted=0 fingerprint_failed=0` | A scan whose files already carried a fingerprint and needed no backfill. Compare against `indexed`: if `fingerprinted + fingerprint_failed` is below `indexed` on a first scan, the files were already fingerprinted. |
| A scan logs `Fingerprinting did not run for every indexed file` with `fingerprint_failed=N` | fpcalc ran and produced nothing for N files — unreadable, truncated, or not real audio. The per-file line names each path and the underlying error. |
| An acquisition shows `unscored` where a score was expected | `ACOUSTID_API_KEY` is empty, or the lookup failed. Both are logged: the worker logs `AcoustID lookup failed`, and the job's own log carries the error too. `unscored` is the honest value — it is what stops a failed lookup reading as a confident zero. |
| yt-dlp fallback fails with a proxy/403 error for a site you trust | The egress boundary's allowlist refused it. Add the host to `ops/squid/allowed-domains.txt` and `docker compose ... up -d egress-proxy` to reload. To run without the boundary entirely (not recommended), set `YTDLP_PROXY=` empty in `.env`. |
| Every acquisition fails with `ssrf: no public IP found for netrunner-slskd` | `ALLOW_PRIVATE_TARGETS` is missing or `false`. slskd is reached by its compose service name, which resolves to a private IP and trips the SSRF guard. `docker-compose.yml` sets it for both app services; keep it if you write your own compose file. |
| Every acquisition fails with `401 Unauthorized`, and slskd logs `Unknown API key beginning with: …` | The key is half-wired: `SLSKD_API_KEY` reached the app but not slskd, so the two sides disagree. It must be set on the slskd service as well. Set it once in `.env` and let `docker-compose.yml` pass it to both. (`SLSKD_API_URL`-era guides suggest `web.authentication.api_keys`; that map is awkward to express as an env var, whereas slskd's primary key is a plain `SLSKD_API_KEY`.) |
| Imports fail with `Downloaded file not found: downloads/…` although slskd reports `Completed, Succeeded` | slskd is downloading into a directory the worker cannot see — by default `~/downloads`, i.e. its **own** `/app/downloads`, not the shared volume the worker imports from. `docker-compose.yml` sets `SLSKD_DOWNLOADS_DIR=/downloads` for this reason. Confirm with `docker compose … exec slskd ls /downloads`. |
| slskd exits at startup, log shows `API key must be between 16 and 255 characters` | `SLSKD_API_KEY` is shorter than 16 characters. The exit code is **0**, so it looks like a clean stop — check `docker inspect <slskd> --format '{{.State.ExitCode}}'`. |
| Acquisition job fails with `panic: ... nil pointer dereference` and `N/N items pending retry` | Fixed: the pipeline held a typed-nil library client. If you see it, you are on a build older than the library-client fix. |
| Watchlist "Sync" fails with `unsupported job type: watchlist_sync` | Fixed: the API created `watchlist_sync` while the worker handles `sync`. A watchlist sync now also needs its provider's credentials (e.g. `LASTFM_API_KEY`); a provider error like `last.fm api returned status: 400` means the dispatch worked and the key is missing. |
| `409 a library already exists at this path` | The path is registered to a different account — `existing_library` in the response names it. Re-create it as that account (which returns the row unchanged), or use another path. |
| A job stays `running` after `POST /api/jobs/:id/cancel` | Older builds wrote the `cancelled` state but the worker never re-read it, so a running job ignored the request. Current builds abort within one item. |
| Imports succeed but the music client shows nothing | The library has not been scanned since the import, or its path is not the volume the worker imports into (`/app/music`). Check for a `scan` job under `/jobs`, and that the library path matches the shared music volume. |
| `/rest/getIndexes` returns `404 Cannot GET` | Every `/rest` route needs its `.view` suffix (`/rest/getIndexes.view`). The bare form reads like a missing route rather than a naming rule. |
