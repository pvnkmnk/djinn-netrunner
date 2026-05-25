# Watchlist Providers

NetRunner supports multiple watchlist sources. Each provider fetches tracks from an external service and feeds them into the acquisition pipeline.

## Provider Reference

| Source Type | Provider | Config (Source URI) | Auth Required |
|---|---|---|---|
| `spotify_playlist` | SpotifyProvider | Playlist URL or URI | None (public) or sp_dc (private) |
| `spotify_liked` | SpotifyProvider | `liked` | sp_dc cookie or OAuth |
| `spotify_discover` | SpotifyProvider | Playlist name (e.g. `Discover Weekly`) | sp_dc cookie |
| `lastfm_loved` | LastFMProvider | Last.fm username | `LASTFM_API_KEY` env var |
| `lastfm_top` | LastFMProvider | Last.fm username | `LASTFM_API_KEY` env var |
| `listenbrainz_listens` | ListenBrainzProvider | ListenBrainz username | `LISTENBRAINZ_TOKEN` env var (optional) |
| `discogs_wantlist` | DiscogsProvider | Discogs username | `DISCOGS_TOKEN` env var |
| `lidarr_wanted` | LidarrProvider | `wanted` | `LIDARR_URL` + `LIDARR_API_KEY` env vars |
| `rss_feed` | RSSProvider | Feed URL | None |
| `local_file` | FileWatchlistProvider | Path to text file | None |
| `local_directory` | DirectoryWatchlistProvider | Path to directory | None |

## Spotify Authentication

Spotify uses a two-pronged authentication strategy (ported from [Stash](https://github.com/rawnaldclark/Stash)):

### Prong 1 — Client Credentials (public data)

Uses well-known SpotDL client credentials to access `api.spotify.com/v1` for public playlists. No user login required.

### Prong 2 — sp_dc Cookie (user-specific data)

For private playlists, Liked Songs, and algorithmic playlists (Discover Weekly, Daily Mixes), the user must provide their `sp_dc` browser cookie.

**How to obtain the sp_dc cookie:**
1. Open [open.spotify.com](https://open.spotify.com) in your browser and sign in
2. Open DevTools (F12) → Application → Cookies → `https://open.spotify.com`
3. Find the `sp_dc` cookie and copy its value
4. Submit it via the Watchlists page "Spotify Connection" section or `POST /api/auth/spotify/spdc`

The sp_dc cookie is long-lived (~1 year) but may need to be refreshed if Spotify invalidates the session.

### Legacy — OAuth

Users with Spotify Developer Apps can still use the existing OAuth flow (`/api/auth/spotify/login`). This serves as a fallback when sp_dc is not configured.

### Fall-through Strategy

- `spotify_playlist`: Prong 1 (client credentials) → Prong 2 (sp_dc) → OAuth
- `spotify_liked`: Prong 2 (sp_dc) → OAuth
- `spotify_discover`: Prong 2 (sp_dc) only

## GraphQL Hash Management

The sp_dc auth path uses Spotify's GraphQL Partner API with persisted query hashes. These are SHA256 hashes of GraphQL operations from Spotify's web player JavaScript bundles.

### Current Hashes

| Constant | Hash | Operation |
|---|---|---|
| `hashLibraryV3` | `973e511c...` | Fetch user's library playlists |
| `hashFetchPlaylist` | `32b05e92...` | Fetch tracks from a specific playlist |
| `hashFetchLibraryTracks` | `087278b2...` | Fetch user's Liked Songs |
| `hashHome` | `23e37f2e...` | Fetch home feed (Discover Weekly, Daily Mixes) |

Located in: `backend/internal/services/spotify_spdc.go` (lines 91–94)

### When Hashes Break

Spotify updates these hashes when they deploy new web player JavaScript bundles. If GraphQL requests start returning errors:

1. Open [open.spotify.com](https://open.spotify.com) in Chrome
2. Open DevTools → Network → filter by `pathfinder`
3. Navigate to your Library, a playlist, Liked Songs, and the Home page
4. Find the `operationName` in each request's query parameters
5. Copy the `sha256Hash` from the `extensions.persistedQuery` field
6. Update the constants in `spotify_spdc.go`

Alternatively, check the [Stash](https://github.com/rawnaldclark/Stash) repo for updated hashes — they track the same endpoints.

### TOTP / Cipher Version

The sp_dc token exchange requires a TOTP code generated with Spotify's XOR cipher (version 61, defined in `spotify_totp.go`). If Spotify changes the cipher version, the `totpCipherVersion` constant and possibly the `totpCipherBytes` need updating. The Stash repo is the reference implementation for these values.
