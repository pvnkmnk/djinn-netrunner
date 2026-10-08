#!/usr/bin/env python3
"""E2E-only MusicBrainz stand-in.

Why this exists
---------------
`e2e/tests/artist-picker.spec.ts` was the only spec driving the Add Artist
picker, and it searched the REAL musicbrainz.org. A third-party outage turned
into a red CI run on an unrelated commit. The spec could not simply skip when
MusicBrainz was unreachable, because `scripts/e2e_gate.sh` holds
`DECLARED_SKIPS` exact in BOTH directions -- a conditional skip either fails the
gate as an undeclared skip, or fails it as a declared skip that stopped running.
So the dependency had to be removed rather than tolerated.

The seam is `MUSICBRAINZ_URL`: `MusicBrainzService` reads it and points at this
service instead. One config field, no cache seeding, no production behaviour
change -- the default is still the public service, and nothing reads the
override except that one constructor.

What it serves
--------------
Only the two endpoints the service calls:

    GET /ws/2/artist?query=artist:<name>&fmt=json&limit=5
    GET /ws/2/artist/<mbid>?fmt=json

Pure stdlib, mirroring ops/fake-slskd. No network, no clock dependence, no
fixtures on disk: the same answer every run, which is the whole point.

The search answer is a FIXED list containing more than one plausible match for
"Boards of Canada", because the picker renders a list of one for a unique name
and a list of one is exactly what this spec was written to prove wrong.
"""
import json
import os
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, unquote, urlparse

PORT = int(os.environ.get("PORT", "8082"))

# One canonical entry per artist, keyed by the MBID the service will be handed.
# `id` is what the picker posts back and what GetArtist is asked for, so the two
# endpoints have to agree -- a fake whose search and lookup disagree would make a
# spec pass or fail for reasons that have nothing to do with the picker.
ARTISTS = [
    {
        "id": "e2e-boards-of-canada",
        "name": "Boards of Canada",
        "sort-name": "Boards of Canada",
        "disambiguation": "Scottish duo",
        "country": "GB",
        "type": "Group",
    },
    {
        # Same name, different entity. This is the case DJI-537 was filed about:
        # an ambiguous name must reach the operator as a list, never as a
        # silently-chosen first result.
        "id": "e2e-boards-of-canada-tribute",
        "name": "Boards of Canada",
        "sort-name": "Boards of Canada",
        "disambiguation": "tribute act",
        "country": "US",
        "type": "Group",
    },
    {
        "id": "e2e-boa-",
        "name": "Boa",
        "sort-name": "Boa",
        "disambiguation": "",
        "country": "NO",
        "type": "Group",
    },
]

# Every query the e2e stack has ever used maps here, so a spec that searches for
# a name this fake does not know still gets a deterministic answer rather than a
# 404 that would read as "MusicBrainz has nothing under that name" -- which is a
# different product state, and a different bug.
EXTRA = {
    "napalm death": {
        "id": "e2e-napalm-death",
        "name": "Napalm Death",
        "sort-name": "Napalm Death",
        "disambiguation": "",
        "country": "GB",
        "type": "Group",
    },
    "death": {
        "id": "e2e-death-disambiguation-a",
        "name": "Death",
        "sort-name": "Death",
        "disambiguation": "",
        "country": "US",
        "type": "Group",
    },
}


def search_answer(query):
    """Answer a search with the list a spec is meant to choose between."""
    name = query.strip().lower()
    if name in EXTRA:
        return [EXTRA[name]]
    if "boards of canada" in name:
        return [a for a in ARTISTS if a["name"] == "Boards of Canada"]
    if "boa" in name:
        return [ARTISTS[2]]
    # Unknown query: a single deterministic entry derived from the query, so the
    # picker still exercises its list-of-one path instead of erroring.
    slug = re.sub(r"[^a-z0-9]+", "-", name).strip("-") or "unknown"
    return [{
        "id": "e2e-" + slug,
        "name": query.strip(),
        "sort-name": query.strip(),
        "disambiguation": "",
        "country": "GB",
        "type": "Group",
    }]


def by_id(mbid):
    for a in ARTISTS + list(EXTRA.values()):
        if a["id"] == mbid:
            return a
    return None


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _send(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):
        # One line per request, like any other service in the stack. Silently
        # swallowing them would make a spec's traffic invisible in the compose
        # logs, which is where you look when a picker test misbehaves.
        print("fake-musicbrainz " + (fmt % args), flush=True)

    def do_GET(self):
        parsed = urlparse(self.path)
        path = parsed.path

        if path == "/health":
            self._send(200, {"status": "ok"})
            return

        m = re.fullmatch(r"/ws/2/artist/([^/]+)", path)
        if m:
            artist = by_id(unquote(m.group(1)))
            if artist is None:
                # MusicBrainz answers 404 for an unknown MBID, and the service
                # turns that into ErrArtistNotFound -- which is the path the
                # re-point handler relies on.
                self._send(404, {"error": "Not Found"})
                return
            self._send(200, artist)
            return

        if path == "/ws/2/artist":
            raw = (parse_qs(parsed.query).get("query") or [""])[0]
            # The service sends `artist:<name>`; accept a bare name too.
            query = raw[len("artist:"):] if raw.startswith("artist:") else raw
            self._send(200, {"artists": search_answer(query), "count": len(search_answer(query))})
            return

        self._send(404, {"error": "not implemented by the e2e stand-in: " + path})


if __name__ == "__main__":
    server = ThreadingHTTPServer(("0.0.0.0", PORT), Handler)
    print("fake-musicbrainz listening on %d" % PORT, flush=True)
    server.serve_forever()