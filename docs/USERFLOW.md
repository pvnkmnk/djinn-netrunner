# User Flow & Experience Guide

What a first-time user does in NetRunner, in order, and what the interface
promises them at each step. Every flow below was driven through the real UI
against a live stack; the [Known rough edges](#known-rough-edges) section lists
what still trips a new user, including the parts this pass could not fix.

Deployment itself is in [`docs/BETA_DEPLOYMENT.md`](BETA_DEPLOYMENT.md); day-two
operations are in [`docs/RUNBOOK.md`](RUNBOOK.md).

## Who this is for

Someone who wants their music library to fill itself: watch a source, acquire
what it finds, verify it is the right recording, and drop it into a library a
media player can read. The UI is a server-rendered operations panel, not a
media browser — Navidrome (or any Subsonic client) is where you listen.

## First session, in order

### 1. Sign in

`/` is the dashboard: job counts, recent activity, and the console. The other
browser routes redirect you here until you have a session; the Subsonic
endpoint (`/rest`) does not use it, and authenticates per request instead. The nav is Watchlists,
Libraries, Playlists, Artists, Jobs, Admin; the footer shows the running
version, so you can tell what you are looking at.

### 2. Watchlists — where music comes from

`/watchlists` is the first page that asks you to make a decision.

1. **Add Watchlist** opens a modal: name, source type, source URI, quality
   profile.
2. Source type picks the provider (`rss_feed`, `lastfm_top`, `spotify_playlist`,
   `local_directory`, …). The Source URI hint updates as you change it, so the
   expected shape is visible before you commit.
3. **Quality Profile** defaults to *Use global default*, which stores the
   admin's default profile. Leave it alone unless you want this source held to
   a different standard.
4. **Save** closes the modal and refreshes the list in place.

Each card offers **Sync** (run the source now), **Preview** (what it would
acquire, before acquiring it), **Edit** and **Delete**. Preview first when you
are unsure: it is read-only and much cheaper than a bad acquisition.

The *Spotify Connection* disclosure at the top matters for the `spotify_*`
types — private playlists and Liked Songs need the `sp_dc` cookie pasted from
your browser, and the badge tells you whether it is linked. Public Spotify
playlists work without it.

A refused save keeps the modal open and shows the reason inside it, so nothing
fails silently.

### 3. Libraries — where music lands

`/libraries` points at directories NetRunner may write to.

1. **Add Library** → name + absolute path. The path must exist and be a
   directory; a path already used by another owner is refused rather than
   silently shared.
2. **Scan** indexes what is there; **Browse** opens the track list; **Enrich**
   fills in metadata.

Tracks stream from `/tracks/:id/stream`, which is also what a Subsonic client
uses.

### 4. Profiles — the quality bar

`/profiles` holds lossless/bitrate/format rules. One profile is the default and
applies wherever a watchlist or artist does not name its own. Creating a
profile is optional: the seeded default is enough to start.

### 5. Schedules — hands-off syncing

`/schedules` attaches a cron expression to a watchlist. A fresh schedule shows
`Next: not scheduled` until the scheduler computes the next run — that is
expected, not a failure. **Disable** pauses it without deleting it.

### 6. Artists — watch a discography

`/artists` tracks an artist (by MusicBrainz identity) rather than a feed. Adding
one requires MusicBrainz to know the name; an unknown artist is refused in the
modal with that reason. A newly added artist shows `Last Scan: Never` until its
first scan.

### 7. Jobs and the console — what actually happened

`/jobs` is the history: every acquisition, sync, scan and enrichment with its
state. A job that was **abandoned** carries the reason — the interesting ones
are refusals, where a downloaded file was playable but not the work that was
asked for. That is the identity gate doing its job, not a bug: the item is
rejected and the library is left alone. The console streams the live log for a
running job.

### 8. Listening

Point a Subsonic client (Navidrome, Symfonium, Feishin) at the stack: `/rest`
for the Subsonic API, `/tracks/:id/stream` for direct playback. The library you
see in your client is the library NetRunner verified.

## What the interface promises

- **Server-rendered partials.** One region owns one section; a mutation
  re-renders that region's contents in place, so the list you see is the state
  the server just produced. No page reload, no duplicated controls.
- **Nothing fails silently.** A rejected save shows the server's message where
  you are looking — inside the modal for forms, in the region for a failed
  load. `4xx` responses are never swallowed.
- **Refusals are visible outcomes.** A gate refusal ends the item through the
  normal terminal path and says so in the job, rather than retrying forever or
  failing as a bare downloader error.
- **One version.** The footer shows the build's version, and it is the release
  you deployed.

## Known rough edges

- **Preview is not enforced.** Nothing stops you from adding a source whose
  provider will always refuse for configuration reasons (a Spotify type with no
  `sp_dc` linked). Preview tells you before a sync does.
- **Schedules need a watchlist.** With no watchlists, the schedule modal's
  Watchlist select is empty and the save is refused; create a watchlist first.
- **`Last Scan: Never` is sticky** until a scan runs — adding an artist does not
  scan it immediately.
- **Library paths are container paths.** A path that exists on your host but
  not inside the container is refused; that is the validation working, but the
  message does not say "inside the container".
