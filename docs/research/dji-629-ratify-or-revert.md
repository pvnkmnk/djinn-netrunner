# DJI-629 — What did the provenance PR change that its predecessor did not, and which way should the ratify-or-revert call go?

## Decision: ratify both. — idols, 2026-10-08

Both changes stand. Neither is reverted.

### Change 1 — the Sync button's swap target: **ratified**

```diff
  ops/web/templates/partials/artists.html
- hx-swap="none"
+ hx-target="#notice"
```

The button was demonstrably inert — `hx-swap="none"` fetched the server's answer and discarded it. Reverting would reinstate a control that appears to work and does nothing, which is worse than the duplication that caused it. The objection that `#notice` is a second announcement channel the Add path does not use is fair, and is recorded below as the follow-up rather than as a reason to revert.

### Change 2 — `DeleteMonitoredArtist` removes `tracked_releases`: **ratified**

```go
tx.Where("artist_id IN (?)", scope.Select("id")).Delete(&database.TrackedRelease{})
```

The 500 on `fk_monitored_artists_releases` was real, and #334 is precisely what turned "scan, then remove" from a rare sequence into the ordinary one. The blast-radius objection is answered by where the code now lives: the cleanup is in `DeleteMonitoredArtist` with the rest of the artist's lifecycle, not in the add path.

### The follow-up this decision creates

Ratifying the delete makes its incompleteness easier to miss, so record it where it will be found: **the delete cleans up `tracked_releases` but NOT the scan job it queued.** The two cleanup concerns are half-implemented, and a reader of `DeleteMonitoredArtist` can reasonably assume both are handled. That gap is DJI-601's to close, not this ticket's.

### What this ticket is *not*

Ratifying does not close DJI-601, does not retire the artists.html / artist-card.html duplication, and does not say the `#notice` announcement channel is settled. Both changes were disclosed in #334's body at the time; the complaint was that they landed without a deliberate second opinion. This is that second opinion.

Closing as **Done** on the decision itself. No code changes were required by this decision.
