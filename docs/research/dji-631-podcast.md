# DJI-631 — What would podcast support actually require, and does it justify NR26?

**Type:** research · ticket NR26 (`DJI-620`) · **Verdict: implement.** The operator has
ruled podcasts **in scope**; the job below is to size the build honestly, not to argue
about whether to do it.

The eight endpoints are real, unimplemented, and owned:

```
| podcast | 8 | NR26 (DJI-620) |                    (docs/OPENSUBSONIC_COVERAGE.md:82)
The `getPodcastEpisode` extension in the ledger is owned by NR26 as well.   (:86)
```

## The eight endpoints

Every one carries `status | gap | not in the route table · **NR26**`. What each does is
Subsonic API knowledge, not something the repo states (see *Could not verify*) — the repo
evidence is the gap row and the endpoint name.

| endpoint | what it does | net-new or reusable |
|---|---|---|
| `getPodcasts` (`:150`) | list channels + their episodes | net-new: model, query, response type |
| `getNewestPodcasts` (`:142`) | newest episodes across channels, `count` | net-new: query only, reuses the above |
| `getPodcastEpisode` (`:149`) | one episode by id — **and an Extension** | net-new |
| `createPodcastChannel` (`:107`) | add a channel by feed URL | net-new: write path + validation |
| `deletePodcastChannel` (`:113`) | remove a channel and its episodes | net-new: cascade delete |
| `deletePodcastEpisode` (`:114`) | remove one stored episode | net-new |
| `downloadPodcastEpisode` (`:118`) | queue/report an episode download | net-new, and **the large one** |
| `refreshPodcasts` (`:171`) | re-fetch all feeds now | partially reusable: the job/schedule system |

`getPodcastEpisode` is gated by an extension of the same name (`:149`, ledger `:206`), so
being implemented is not the same as being *discoverable* — see the NR05 note below.

## What it needs that does not exist

**Nothing podcast-shaped exists.** A search for `Podcast|podcast` across all `*.go` and
`*.sql` returns only unrelated Spotify fixtures:

```
backend/internal/services/spotify_graphql.go:72:  // Filtered items (local tracks, podcasts) reduce len(tracks) but
```

There is no `PodcastChannel`, no `PodcastEpisode`, no handler, no migration, no route. The
models that exist are music-shaped — `Library`, `Track`, `Playlist`, `MonitoredArtist`,
`TrackedRelease` (`backend/internal/database/models.go`). Reusing `Track` is not open:
`Track.Path` is `gorm:"uniqueIndex;not null"` and the model is `album`/`artist`/`track_num`
identity — an episode has none of that.

Missing, concretely:

1. **Two models + migration** — a channel (feed URL, title, image, last-refresh) and an
   episode (guid, enclosure URL, published, duration, status). Plus the SQL bootstrap DDL
   under `ops/db/init/` to match, since this repo keeps both paths.
2. **A podcast ingest service** — fetch, parse, upsert channel + episodes.
3. **Episode storage + delivery** — `downloadPodcastEpisode` implies bytes on disk with a
   status, and `stream`/`download`-style retrieval for them.
4. **A refresh cadence** — `refreshPodcasts` plus polling.
5. **New response types.** `subsonicResponse` is a fixed envelope with one pointer field
   per response family (`backend/internal/api/subsonic.go:51-67`); podcasts need new fields
   added to it and new types beside it.
6. **An ownership decision** — this is the real design question. Every comparable NetRunner
   surface is owner-scoped (`owner_user_id` on `Library`, `Playlist`, `MonitoredArtist`),
   but podcast channels are global to a Subsonic server. Choose deliberately, because
   `getPodcasts` returns server-wide data in the spec.

## What it can reuse

- **The fetch/parse path already exists and is pointed at the right client.**
  `RSSProvider` uses `github.com/mmcdole/gofeed`
  (`backend/internal/services/rss_provider.go:9`), `NewRSSProvider(httpClient)` (`:19`),
  `fp.ParseURLWithContext(watchlist.SourceURI, ctx)` (`:28`), and is registered for
  `rss_feed` **with the SSRF-safe client**:

  ```
  safeProxyClient := NewSafeProxyAwareHTTPClient(cfg, 30*time.Second)   (watchlist_service.go:35)
  s.RegisterProvider("rss_feed", NewRSSProvider(safeProxyClient))       (watchlist_service.go:49)
  ```

  **But its projection discards exactly what podcasts need.** `FetchTracks` returns only

  ```
  "artist", "title", "cover_art_url", "source_link"     (rss_provider.go:57-62)
  ```

  — no `guid`, no `published`, no `duration`, no `enclosure` URL, no description. Those are
  precisely the episode fields, and `gofeed`'s `Item` carries them. So the *parser* is
  reusable; the *projection* is not, and a new one is required rather than a tweak.
- **The outbound-fetch boundary** — `backend/internal/services/safe_http.go`: the
  `privateCIDRs` list, `ErrDisallowedDestination`, the `safeAddressValidator` RoundTripper
  and `redirectHopLimit = 10`. Reuse as-is; do not build a second fetcher.
- **The `/rest` surface and its auth.** Routes are one line each under a group gated by
  `if cfg.Subsonic.Enabled {` (`backend/cmd/server/main.go:476`), each with
  `subsonicHandler.AuthMiddleware`; the XML/JSON dispatch is `respond` (`subsonic.go:69`),
  so a new handler is mostly query + response-shape work.
- **The job/schedule system** for `refreshPodcasts` — there is already a job-type registry
  (`backend/internal/database/job_types.go:25`) and a `Schedule` model, so a
  `podcast_refresh` type rides existing machinery rather than inventing a cron.
- **`AudioProbe`** (`backend/internal/services/audio_probe.go`) if downloaded episodes are
  to be validated before import.

## Build sketch

| slice | size | note |
|---|---|---|
| Models + migration + SQL bootstrap | S | 2 models, 1 migration |
| `PodcastService`: fetch, parse, upsert | M | new projection over `gofeed`; reuse `safeProxyClient` |
| Response types + 8 handlers + routes | M–L | mechanical, but 8 endpoints and their XML shapes |
| `downloadPodcastEpisode` + storage + retrieval | **L** | the honest large part: bytes, status, and a delivery path |
| `refreshPodcasts` + cadence | S–M | rides the job system |
| UI/admin (optional, not required by the spec) | M | Subsonic's own create/delete are the admin surface |
| Tests: handler tests + an e2e spec | M | the repo's convention is handler tests plus Playwright |

**Honest size: 4–6 PRs, comfortably a wave of its own** — not a single PR, and the bulk is
the download/storage path rather than the eight routes. The route work is mechanical and
cheap; `downloadPodcastEpisode` is where the real cost sits, because it introduces a second
kind of stored audio that the acquisition pipeline's music identity model does not describe.

**Cross-ticket dependency worth flagging:** `getPodcastEpisode` is extension-gated, and the
endpoint that *advertises* extensions — `getOpenSubsonicExtensions` — is itself an open gap
owned by **NR05** (`docs/OPENSUBSONIC_COVERAGE.md`, Finding 3), which is still a gap row:

```
| `getOpenSubsonicExtensions` | get+post | Addition, System | — | — | gap | not in the route table · **NR05** |
```

Finding 3 states the consequence directly: "A client cannot ask which of the 11 extensions
this server supports, so every extension below is invisible rather than absent." So NR26 can
implement `getPodcastEpisode` and no client will know to call it until NR05 lands. NR05 is
not a hard blocker for the other seven, which are base API.

## Podcast vs chat vs jukebox

- **podcast — the only one of the three that adds content.** Chat and jukebox add verbs over
  content that already exists; podcasts add a *kind* of audio the library has never held.
  That is what makes it the closest of the three to load-bearing: it is a product expansion
  (spoken-word acquisition through the same appliance), not a checkbox.
- **chat — protocol completeness.** `getChatMessages` / `addChatMessage` are a server-wide
  chatroom overlay. Nothing in NetRunner's domain (watchlists → acquisition → library →
  playback) touches it.
- **jukebox — protocol completeness.** `jukeboxControl` drives playback on a server-side
  "jukebox" endpoint. NetRunner delegates playback to Subsonic clients, so there is no
  appliance-side player for it to control.

Worth saying plainly, since the operator ruled in scope: **none of the three is load-bearing
for a music appliance in the strict sense** — a "music appliance" is complete without all
three. Podcast is load-bearing for a *media* appliance. That distinction is the reason
podcast is the one that should be built and the other two are candidates for a
recorded-unsupported close; it is not a reason to reopen the in-scope ruling.

## Disposition

**Implement.** The operator ruled podcasts in scope and this research defines the
requirement. Concretely for NR26:

1. Land the models, migration and SQL bootstrap first — they unblock everything else.
2. `PodcastService` with its own `gofeed` projection, through `safeProxyClient`.
3. The seven base-API endpoints.
4. The download/storage path as its own PR; decide owner-scoping before writing it.
5. `getPodcastEpisode` can land with NR26 but only becomes *discoverable* once NR05 ships
   `getOpenSubsonicExtensions` — record that dependency on both tickets rather than assuming
   NR26 alone completes the feature.

## Could not verify

- **What each endpoint does and what a conformant client loses without them.** The pinned
  spec is not vendored in the repo; `docs/OPENSUBSONIC_COVERAGE.md` carries only the
  endpoint name, verbs and tags. The behavioural descriptions above are Subsonic API
  knowledge, not a repo quote, and should be checked against the pinned revision
  (`SPEC_VERSION = "1.16.1"`, sha256 `cb54c03c…`) before the handlers are written. This is
  the single largest gap in this note.
- **Whether `gofeed`'s `Item` has the exact fields needed.** The claim that the projection
  discards `guid`/`published`/`duration`/`enclosure` is a claim about what is *not* in the
  map at `rss_provider.go:57-62`; whether `gofeed` exposes each one (and in which form for
  Atom vs RSS) was not confirmed by reading the library.
- **Existing job-type registry contents**, i.e. whether a `podcast_refresh` entry would be a
  pure addition or collide with a name in use.
- **Whether any Subsonic client in scope actually consumes podcasts.** No client inventory
  exists in the repo, so "what a client loses" is inferred from the spec, not measured.

## What would falsify this verdict

- The pinned spec showing the eight endpoints are not required for `getPodcasts`-style
  conformance — e.g. if they sit behind an extension the ledger mis-attributes to base API.
- Evidence that no client NetRunner cares about calls them, which would move the disposition
  from *implement* to *record as deliberately unsupported* — the outcome
  `OPENSUBSONIC_COVERAGE.md` explicitly permits ("a ticket may legitimately close by
  recording its endpoints as deliberately unsupported, with the reason").
- The download/storage path turning out to require reworking the music identity model
  (`canonical_identity.go`, `importFile`) rather than sitting beside it — that would raise
  the size materially and split NR26 into two tickets.
