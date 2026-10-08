#!/usr/bin/env python3
"""Generate docs/OPENSUBSONIC_COVERAGE.md from the pinned OpenSubsonic spec.

The spec revision is pinned in SPEC_VERSION / SPEC_SHA256 below. The document
is generated so that the mechanical half (which endpoints exist, what they are
called, which extension gates them) cannot drift from the spec, and so that the
judgement half (is this implemented, who owns the gap) lives in version control
as reviewable data rather than as prose.

Re-run when the pin moves:

    curl -sSL -o .osp.json https://opensubsonic.netlify.app/docs/openapi/openapi.json
    py scripts/opensubsonic_coverage.py .osp.json ops/web/docs/OPENSUBSONIC_COVERAGE.md

Route coverage is measured off the registration sites in backend/cmd/server/main.go
and is cross-checked by backend/cmd/server/opensubsonic_coverage_test.go, which
fails if this document and app.GetRoutes() disagree.
"""

from __future__ import annotations

import hashlib
import json
import re
import sys
from pathlib import Path

# --- the pin -----------------------------------------------------------------
SPEC_VERSION = "1.16.1"
SPEC_SHA256 = "cb54c03c33835d132c555863e9771e30dfaa2930312853ca27dfece2ed46bfb6"
SPEC_URL = "https://opensubsonic.netlify.app/docs/openapi/openapi.json"
SPEC_DOCS_REPO = "https://github.com/opensubsonic/open-subsonic-api"

# Endpoint name -> the extensions the spec documents for the server as a whole.
# None of these are implemented; the ledger in the document says so explicitly.
EXTENSIONS = [
    ("apiKeyAuthentication", "NR05", "tokenInfo"),
    ("formPost", "NR24", "POST on 83 paths"),
    ("getPodcastEpisode", "unowned", "getPodcastEpisode"),
    ("indexBasedQueue", "NR10", "getPlayQueueByIndex / savePlayQueueByIndex"),
    ("playbackReport", "NR10", "reportPlayback"),
    ("songLyrics", "NR22", "getLyricsBySongId"),
    ("sonicSimilarity", "NR06", "getSimilarSongs2 / getSonicSimilarTracks"),
    ("templating", "NR12", "template parameters"),
    ("topSongsByArtistId", "NR06", "getTopSongs by artistId"),
    ("transcodeOffset", "NR20", "byte-range transcode offsets"),
    ("transcoding", "NR20", "getTranscodeStream / getTranscodeDecision"),
]

# Endpoint name -> (handler, behavioural test). Every entry here is asserted to
# exist as a route by the Go guard; the test column is a claim about coverage
# that a reviewer can check with `go test -run`.
IMPLEMENTED = {
    "ping": ("backend/internal/api/subsonic.go", "TestSubsonic_Ping, TestSubsonic_Ping_JSON, e2e subsonic.spec.ts 'Ping succeeds with valid auth'"),
    "getLicense": ("backend/internal/api/subsonic.go", "TestSubsonic_License — aliased at /rest/license.view, see Finding 1"),
    "getIndexes": ("backend/internal/api/subsonic.go", "TestSubsonic_GetIndexes_{Empty,WithArtists,ArtistsAZ,JSON}"),
    "getMusicDirectory": ("backend/internal/api/subsonic.go", "TestSubsonic_GetMusicDirectory_{MissingID,ArtistDirectory,AlbumDirectory,TrackDirectory,NotFound}"),
    "getArtist": ("backend/internal/api/subsonic.go", "TestSubsonic_GetArtist_{Found,MalformedID,NotFound}"),
    "getAlbum": ("backend/internal/api/subsonic.go", "TestSubsonic_GetAlbum_{Found,MalformedID,NotFound}"),
    "getSong": ("backend/internal/api/subsonic.go", "TestSubsonic_GetSong_{Found,NotFound,BadUUID,MissingID,JSON}"),
    "search3": ("backend/internal/api/subsonic.go", "TestSubsonic_Search3_{MissingQuery,EmptyResults,WithResults,WithPagination,JSON}"),
    "getAlbumList2": ("backend/internal/api/subsonic.go", "TestSubsonic_GetAlbumList2_{Random,Newest,AlphabeticalByName,AlphabeticalByArtist,Empty,WithPagination,JSON}"),
    "getRandomSongs": ("backend/internal/api/subsonic.go", "TestSubsonic_GetRandomSongs_{Normal,Empty,WithSize}"),
    "getCoverArt": ("backend/internal/api/subsonic.go", "TestSubsonic_GetCoverArt_{SSRBlocked,MissingID,InvalidID,NotFound}"),
    "stream": ("backend/internal/api/subsonic.go:871", "NONE — see Finding 2"),
    "getScanStatus": ("backend/internal/api/subsonic.go", "TestSubsonic_GetScanStatus"),
    "startScan": ("backend/internal/api/subsonic.go", "TestSubsonic_StartScan"),
    "getPlaylists": ("backend/internal/api/subsonic.go", "TestSubsonic_GetPlaylists_{Empty,WithPlaylists,Public}"),
    "getPlaylist": ("backend/internal/api/subsonic.go", "TestSubsonic_GetPlaylist_{MissingID,NotFound,InvalidUUID,Found,AccessDenied}"),
    "createPlaylist": ("backend/internal/api/subsonic.go", "TestSubsonic_CreatePlaylist_{MissingName,New,WithComment,Public,UpdateExisting}"),
    "deletePlaylist": ("backend/internal/api/subsonic.go", "TestSubsonic_DeletePlaylist_{MissingID,InvalidUUID,NotFound,Success,AccessDenied}"),
}

# Routes registered under a name the spec does not define. Each is an
# intentional alias kept for clients written against the older name.
ALIASES = {
    "license": "alias for getLicense — registered at backend/cmd/server/main.go so pre-rename clients keep working",
}

# Endpoint name -> owning ticket for every gap. "unowned" is a finding, not a
# placeholder: those endpoints have no NR ticket behind them and P-DJI-29's
# wave order cannot reach them until one does.
GAPS = {
    # NR05 — truthful identity, extension and management discovery (DJI-565)
    "getOpenSubsonicExtensions": "NR05",
    "tokenInfo": "NR05",
    # NR06 — browsing, search and music lists (DJI-566)
    "getAlbumInfo": "NR06", "getAlbumInfo2": "NR06", "getArtistInfo": "NR06",
    "getArtistInfo2": "NR06", "getArtists": "NR06", "getGenres": "NR06",
    "getMusicFolders": "NR06", "getTopSongs": "NR06", "getVideos": "NR06",
    "getVideoInfo": "NR06", "getSimilarSongs": "NR06", "getSimilarSongs2": "NR06",
    "findSonicPath": "NR06", "getSonicSimilarTracks": "NR06",
    "getAlbumList": "NR06", "getNowPlaying": "NR06", "getSongsByGenre": "NR06",
    "getStarred": "NR06", "getStarred2": "NR06",
    "search": "NR06", "search2": "NR06",
    # NR07 — streaming and seeking (DJI-567)
    "download": "NR07", "hls.m3u8": "NR07", "getAvatar": "NR07",
    # NR08 — metadata and artwork (DJI-568)
    "getCaptions": "NR08",
    # NR09 — playlist operations (DJI-569)
    "updatePlaylist": "NR09",
    # NR10 — annotations, history and saved state (DJI-570)
    "star": "NR10", "unstar": "NR10", "scrobble": "NR10", "setRating": "NR10",
    "reportPlayback": "NR10", "getPlayQueue": "NR10", "savePlayQueue": "NR10",
    "getPlayQueueByIndex": "NR10", "savePlayQueueByIndex": "NR10",
    "createBookmark": "NR10", "deleteBookmark": "NR10", "getBookmarks": "NR10",
    # NR04 — unified authorization (DJI-564)
    "getUser": "NR04", "getUsers": "NR04", "createUser": "NR04",
    "updateUser": "NR04", "deleteUser": "NR04", "changePassword": "NR04",
    # NR20 — transcoding (DJI-581)
    "getTranscodeStream": "NR20", "getTranscodeDecision": "NR20",
    # NR22 — lyrics (DJI-584)
    "getLyrics": "NR22", "getLyricsBySongId": "NR22",
    # NR23 — internet radio (DJI-585)
    "createInternetRadioStation": "NR23", "updateInternetRadioStation": "NR23",
    "deleteInternetRadioStation": "NR23", "getInternetRadioStations": "NR23",
    # NR24 — media sharing (DJI-586)
    "createShare": "NR24", "updateShare": "NR24",
    "deleteShare": "NR24", "getShares": "NR24",
    # Podcast and chat: no NR ticket exists (Finding 4)
    "getPodcasts": "unowned", "getNewestPodcasts": "unowned",
    "createPodcastChannel": "unowned", "refreshPodcasts": "unowned",
    "deletePodcastChannel": "unowned", "deletePodcastEpisode": "unowned",
    "downloadPodcastEpisode": "unowned", "getPodcastEpisode": "unowned",
    "getChatMessages": "unowned", "addChatMessage": "unowned",
    "jukeboxControl": "unowned",
}

EXT_RE = re.compile(r"[Oo]pen[Ss]ubsonic extension name `(?P<e>[a-zA-Z]+)`")


def spec_rows(spec: dict) -> list[dict]:
    rows = []
    for path, ops in spec["paths"].items():
        get = ops.get("get", {})
        m = EXT_RE.search(get.get("description", "") or "")
        rows.append({
            "name": path.removeprefix("/rest/"),
            "verbs": "+".join(sorted(ops)),
            "tags": ", ".join(sorted(set(get.get("tags", [])))),
            "extension": m.group("e") if m else "",
            "deprecated": any(o.get("deprecated") for o in ops.values()),
        })
    rows.sort(key=lambda r: r["name"])
    return rows


def main() -> int:
    if len(sys.argv) != 3:
        sys.stderr.write("usage: opensubsonic_coverage.py <openapi.json> <out.md>\n")
        return 2
    src, dst = Path(sys.argv[1]), Path(sys.argv[2])

    try:
        raw = src.read_bytes()
    except OSError as exc:
        sys.stderr.write(f"cannot read {src}: {exc}\n")
        return 1
    try:
        spec = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        sys.stderr.write(f"{src} is not valid UTF-8 JSON: {exc}\n")
        return 1
    digest = hashlib.sha256(raw).hexdigest()
    if digest != SPEC_SHA256:
        sys.stderr.write(
            f"{src} sha256 is {digest}, not the pinned {SPEC_SHA256}.\n"
            "The upstream document changed without its version changing. Re-read "
            "it before regenerating; do not simply update the pin.\n"
        )
        return 1
    version = spec.get("info", {}).get("version")
    if version != SPEC_VERSION:
        sys.stderr.write(
            f"spec version {version!r} does not match the pin {SPEC_VERSION!r}.\n"
            "Re-read the spec, re-classify the matrix, then update SPEC_VERSION "
            "and SPEC_SHA256 together.\n"
        )
        return 1

    rows = spec_rows(spec)
    total = len(rows)
    have = [r for r in rows if r["name"] in IMPLEMENTED]
    gaps = [r for r in rows if r["name"] in GAPS]
    base = [r for r in rows if not r["extension"]]
    base_gaps = [r for r in base if r["name"] in GAPS]
    unowned = [r for r in rows if GAPS.get(r["name"]) == "unowned"]

    unknown = [
        r["name"] for r in rows
        if r["name"] not in IMPLEMENTED and r["name"] not in GAPS
    ]
    if unknown:
        sys.stderr.write("unclassified endpoints: " + ", ".join(unknown) + "\n")
        return 1
    if len(have) + len(gaps) != total:
        sys.stderr.write(f"matrix is not total: {len(have)} + {len(gaps)} != {total}\n")
        return 1

    out = []
    w = out.append
    w("# OpenSubsonic coverage")
    w("")
    w("Generated by `scripts/opensubsonic_coverage.py`. Do not hand-edit: the")
    w("endpoint inventory is derived from the pinned spec, and the classification")
    w("is the script's data table so it can be reviewed as a diff.")
    w("")
    w("| | |")
    w("|---|---|")
    w(f"| Spec | OpenSubsonic {SPEC_VERSION} |")
    w(f"| Source | `{SPEC_URL}` |")
    w(f"| sha256 | `{SPEC_SHA256}` |")
    w(f"| Docs | {SPEC_DOCS_REPO} |")
    w(f"| Endpoints | {total} paths ({len(base)} base-API GET, "
      f"{len(rows) - len(base)} extension-gated GET) |")
    w("| Re-point the pin | change `SPEC_VERSION` and `SPEC_SHA256` together |")
    w("")
    w("## Where this stands")
    w("")
    w("| | count | share |")
    w("|---|---|---|")
    w(f"| Base-API endpoints | {len(base)} | 100% |")
    w(f"| Base-API implemented | {len(have)} | {len(have) * 100 // len(base)}% |")
    w(f"| Base-API gaps | {len(base_gaps)} | {len(base_gaps) * 100 // len(base)}% |")
    w(f"| Extension-gated endpoints | {len(rows) - len(base)} | — |")
    w(f"| Extensions implemented | 0 of {len(EXTENSIONS)} | 0% |")
    w(f"| Gaps with no owning ticket | {len(unowned)} | — |")
    w("")
    w("## Findings")
    w("")
    w("### 1. `/rest/license.view` was an endpoint no client could reach")
    w("")
    w("The License handler was registered as `/rest/license.view`; the spec's name")
    w("is `getLicense`. A conformant client asking for the licence got a 404 while")
    w("the handler worked fine, and nothing in the suite could see it:")
    w("`subsonic.spec.ts` has a case called *License endpoint works* which passed")
    w("because it called the wrong URL, and `TestSubsonic_License` drove the handler")
    w("directly. Route existence, a handler test and a green browser spec all agreed")
    w("while the feature was unreachable to the clients that need it.")
    w("")
    w("Fixed: `/rest/getLicense.view` is the spec name and `/rest/license.view` is")
    w("kept as an alias, so nothing breaks for a client that learned the old one.")
    w("The guard below fails if either half of that pair disappears.")
    w("")
    w("### 2. `/rest/stream.view` has no test at all")
    w("")
    w("`SubsonicHandler.Stream` (`backend/internal/api/subsonic.go:871`) is the")
    w("endpoint every client actually uses to listen to music, and nothing drives it.")
    w("")
    w("There are nine passing tests named `TestStreamTrack_*` in")
    w("`backend/internal/api/stream_test.go` covering BOLA, admin access, `200`,")
    w("`206` range and the error paths. They test a **different handler** —")
    w("`LibraryHandler.StreamTrack`, registered at `/tracks/:id/stream` — so a name")
    w("match between `Stream` and `StreamTrack` reads as coverage that is not there.")
    w("Streaming is the endpoint whose failure a user notices first, so it is the")
    w("one gap here that matters more than its size.")
    w("")
    w("### 3. Discovery is the base-API hole that hides every other one")
    w("")
    w("`getOpenSubsonicExtensions` is base API (tags `System`, `Addition`, no")
    w("extension requirement) and unimplemented. A client cannot ask which of the")
    w(f"{len(EXTENSIONS)} extensions this server supports, so every extension below is")
    w("invisible rather than absent. `tokenInfo` is extension-gated")
    w("(`apiKeyAuthentication`), so it is correctly counted as an NR05 gap rather")
    w("than a base-API one.")
    w("")
    w("### 4. Eleven endpoints have no ticket behind them")
    w("")
    w("Podcast (8), chat (2) and `jukeboxControl` (1) appear in the spec but in")
    w("none of NR01-NR25. P-DJI-29's wave order cannot reach them, and until a")
    w("ticket exists they cannot be 'cleared' by any wave. Either they are out of")
    w("scope for a library appliance and should be declared so, or they need a")
    w("ticket. This is a scope decision, not an implementation gap.")
    w("")
    w("## Endpoint matrix")
    w("")
    w("`impl` = route registered and behaviourally tested · `gap` = not registered,")
    w("owner named · `unowned` = not registered and no ticket owns it.")
    w("")
    w("| endpoint | verbs | tags | gated by | status | evidence / owner |")
    w("|---|---|---|---|---|---|")
    for r in rows:
        name = r["name"]
        gated = r["extension"] or "—"
        if name in IMPLEMENTED:
            handler, test = IMPLEMENTED[name]
            status = "impl"
            ev = f"`{handler}`<br>{test}"
        else:
            owner = GAPS.get(name, "")
            status = "unowned" if owner == "unowned" else "gap"
            ev = f"not in the route table · **{owner}**" if status == "gap" else "**no ticket**"
        if r["deprecated"]:
            name = f"`{name}` (deprecated)"
        w(f"| `{name}` | {r['verbs']} | {r['tags']} | {gated} | {status} | {ev} |")
    w("")
    w("## Compatibility aliases")
    w("")
    w("Registered alongside a spec endpoint so clients written against an older")
    w("name keep working. These are not spec endpoints and are not counted as")
    w("coverage.")
    w("")
    w("| alias route | serves | why it stays |")
    w("|---|---|---|")
    for name, why in ALIASES.items():
        w(f"| `/rest/{name}.view` | `{name}` in the matrix above | {why} |")
    w("")
    w("## Extension ledger")
    w("")
    w("| extension | what it adds | owner |")
    w("|---|---|---|")
    for name, owner, adds in EXTENSIONS:
        w(f"| `{name}` | {adds} | {owner} |")
    w("")
    w("## Guard")
    w("")
    w("`backend/cmd/server/opensubsonic_coverage_test.go` reads the `impl` rows")
    w("above and compares them with `app.GetRoutes()` under")
    w("`cfg.Subsonic.Enabled = true`. It fails if this document claims an endpoint")
    w("that is not registered, or omits one that is — so the coverage number above")
    w("cannot rot into a claim. It also asserts both `getLicense.view` and the")
    w("`license.view` alias are registered.")
    w("")

    dst.write_text("\n".join(out), encoding="utf-8", newline="\n")
    sys.stdout.write(
        f"{dst}: {total} endpoints, {len(have)} implemented, "
        f"{len(gaps)} gaps, {len(unowned)} unowned\n"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
