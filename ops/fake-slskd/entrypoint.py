#!/usr/bin/env python3
"""E2E-only stand-in for slskd (fake-slskd).

Implements exactly the slskd HTTP API surface the NetRunner worker calls,
driven by query text so a spec can choose what the "network" answers:

  * POST /api/v0/searches                            -> start a search (returns an id)
  * GET  /api/v0/searches/{id}?includeResponses=true -> canned responses
  * POST /api/v0/transfers/downloads/{user}          -> enqueue (returns an id)
  * GET  /api/v0/transfers/downloads/{user}/{id}     -> transfer state (Succeeded)
  * DELETE /api/v0/transfers/downloads/{user}/{id}   -> cancel (accepted)
  * GET  /healthz                                    -> container healthcheck

The downloads directory IS the worker's staging volume: a "completed"
transfer is just the pre-generated file sitting where slskd would have put
it. Two files are generated at startup with ffmpeg:

  * peer "decoy-peer":  "Totally Different Band - Unrelated Record.flac",
    tagged Totally Different Band / Unrelated Record — playable audio that
    is NOT the requested work. Drives the Soulseek entrance's wrong-work
    refusal (the clause with no seam until now).
  * peer "clean-peer":  "Clean Success Artist - Clean Success Album.flac",
    tagged to match its request exactly. Drives the success path: download
    -> gate passes -> import -> library.

Search routing: the fake peer answers when the query contains its marker
words ("Unrelated Record" / "Clean Success"). The decoy's filename shares
NO word with the success request and vice versa — the identity gate folds
names to words, and the wrong-work probe depends on zero overlap.

Auth: the same X-API-Key model as slskd (SLSKD_API_KEY), so the worker's
client is unmodified.
"""
import json
import os
import subprocess
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs, unquote

API_KEY = os.environ.get("SLSKD_API_KEY", "e2e-test-api-key-0123456789abcdef")
DOWNLOADS_DIR = os.environ.get("FAKE_DOWNLOADS_DIR", "/downloads")

PEERS = {
    "decoy": {
        "username": "decoy-peer",
        # The FILENAME carries the query's marker words ("Unrelated Record")
        # so the fake can route the search to this peer — which is exactly
        # how a real Soulseek peer ends up in results, and the signal the
        # identity gate is built to see through. The TAGS name a completely
        # different work: every tag word must share NOTHING with the request
        # on BOTH axes (artist AND album) — the gate rejects only when both
        # disagree, and one equal album tag rescues the file (observed live:
        # the decoy came back "completed (duplicate recording)" with the
        # marker words in its album tag).
        "filename": r"@@abcde\Totally Different Band - Unrelated Record.flac",
        "tag_artist": "Verge Ensemble",
        "tag_album": "Null Hour",
        "tag_title": "Static Bloom",
        "marker": "unrelated record",
        "local": "Totally Different Band - Unrelated Record.flac",
    },
    "clean": {
        "username": "clean-peer",
        "filename": r"@@fghij\Clean Success Artist - Clean Success Album.flac",
        "tag_artist": "Clean Success Artist",
        "tag_album": "Clean Success Album",
        "tag_title": "Clean Success Song",
        "marker": "clean success",
        "local": "Clean Success Artist - Clean Success Album.flac",
    },
}

SEARCHES = {}   # id -> {"state": ..., "responses": [...]}
TRANSFERS = {}  # (username, id) -> {"filename": ..., "size": ...}
LOCK = threading.Lock()


def generate(peer, path):
    """A real 20-second FLAC with the peer's tags; 242 KB, well above the
    64 KiB plausibility floor. The comment tag is unique per generation:
    deterministic bytes would hash identically to a copy an earlier run
    imported, and the hash-duplicate path would short-circuit the identity
    gate this fixture exists to exercise."""
    subprocess.run(
        ["ffmpeg", "-y", "-loglevel", "error",
         "-f", "lavfi", "-i", "sine=frequency=440:duration=20",
         "-metadata", f"artist={peer['tag_artist']}",
         "-metadata", f"album_artist={peer['tag_artist']}",
         "-metadata", f"album={peer['tag_album']}",
         "-metadata", f"title={peer['tag_title']}",
         "-metadata", f"comment={uuid.uuid4().hex}",
         "-c:a", "flac", path],
        check=True,
    )


def peer_for_query(query):
    q = query.lower()
    for peer in PEERS.values():
        if peer["marker"] in q:
            return peer
    return None


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        print("[fake-slskd]", self.address_string(), fmt % args)

    def _json(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _authed(self):
        return self.headers.get("X-API-Key") == API_KEY

    def do_GET(self):
        parsed = urlparse(self.path)
        path = unquote(parsed.path)
        qs = parse_qs(parsed.query)

        if path == "/healthz":
            return self._json(200, {"ok": True})

        if not self._authed():
            return self._json(401, {"error": "invalid API key"})

        if path == "/api/v0/session":
            return self._json(200, {"authenticated": True})

        # GET /api/v0/searches/{id}?includeResponses=true
        if path.startswith("/api/v0/searches/"):
            sid = path.rsplit("/", 1)[1]
            with LOCK:
                search = SEARCHES.get(sid)
            if search is None:
                return self._json(404, {"error": "no such search"})
            return self._json(200, search)

        # GET /api/v0/transfers/downloads/{user}/{id}
        # parsed.path keeps the leading '/', so strip the known prefix instead
        # of splitting on '/' — parts[4] would be "downloads", not the user.
        transfer_prefix = "/api/v0/transfers/downloads/"
        if parsed.path.startswith(transfer_prefix) and "/" in parsed.path[len(transfer_prefix):]:
            rest = parsed.path[len(transfer_prefix):]
            username, tid = unquote(rest).split("/", 1)
            with LOCK:
                t = TRANSFERS.get((username, tid))
            if t is None:
                return self._json(404, {"error": "no such download"})
            # Bytes are already staged: report a completed, succeeded transfer.
            return self._json(200, {
                "id": tid,
                "username": username,
                "filename": t["filename"],
                "size": t["size"],
                "state": "Completed, Succeeded",
                "bytesTransferred": t["size"],
                "bytesRemaining": 0,
                "percentComplete": 100.0,
                "exception": "",
            })

        return self._json(404, {"error": "not found"})

    def do_POST(self):
        parsed = urlparse(self.path)
        path = unquote(parsed.path)

        if not self._authed():
            return self._json(401, {"error": "invalid API key"})

        # POST /api/v0/searches  {searchText, ...}
        if path == "/api/v0/searches":
            length = int(self.headers.get("Content-Length", 0))
            payload = json.loads(self.rfile.read(length) or b"{}")
            query = payload.get("searchText", "")
            peer = peer_for_query(query)
            sid = str(uuid.uuid4())
            responses = []
            if peer is not None:
                responses = [{
                    "username": peer["username"],
                    "uploadSpeed": 1_000_000,
                    "queueLength": 0,
                    "files": [{
                        "filename": peer["filename"],
                        "size": 248000,
                        "isLocked": False,
                        "bitRate": 99,   # lossless indicator for the scorer
                        "length": 20,
                    }],
                }]
            with LOCK:
                SEARCHES[sid] = {"id": sid, "state": "Completed, Succeeded",
                                 "responses": responses}
            return self._json(200, {"id": sid})

        # POST /api/v0/transfers/downloads/{user}  [{filename, size}]
        # Same prefix-stripping as the GET above.
        transfer_prefix = "/api/v0/transfers/downloads/"
        if parsed.path.startswith(transfer_prefix):
            username = unquote(parsed.path[len(transfer_prefix):])
            length = int(self.headers.get("Content-Length", 0))
            payload = json.loads(self.rfile.read(length) or b"[]")
            filename = payload[0]["filename"] if payload else ""
            peer = next((p for p in PEERS.values()
                         if p["filename"].endswith(filename.replace("\\", "/").split("/")[-1])), None)
            if peer is None or peer["username"] != username:
                return self._json(404, {"error": "no such peer file"})
            tid = str(uuid.uuid4())
            with LOCK:
                TRANSFERS[(username, tid)] = {"filename": peer["filename"],
                                              "size": 248000}
            # Re-stage the bytes NOW. The pipeline's terminal discard deletes a
            # staged file when its item finishes with it (the gate refusal is
            # terminal by design, DJI-497), so a file placed once at startup is
            # gone after the first probe run and every later run stat-fails.
            # A transfer that "completes" means the bytes are there when the
            # worker looks — regenerating is what a real peer re-serving does.
            generate(peer, os.path.join(DOWNLOADS_DIR, peer["local"]))
            # slskd answers an enqueue with {"enqueued": [...transfers],
            # "failed": [...]} — the worker decodes exactly that shape.
            return self._json(201, {"enqueued": [{
                "id": tid,
                "username": username,
                "filename": peer["filename"],
                "size": 248000,
                "state": "Queued",
                "bytesTransferred": 0,
                "bytesRemaining": 248000,
                "percentComplete": 0.0,
                "exception": "",
            }], "failed": []})

        return self._json(404, {"error": "not found"})

    def do_DELETE(self):
        parsed = urlparse(self.path)
        if not self._authed():
            return self._json(401, {"error": "invalid API key"})
        # Accept BOTH cancel routes the worker uses: transfer cancellation
        # (abandon path) and search cleanup after every completed search.
        if parsed.path.startswith("/api/v0/transfers/downloads/"):
            return self._json(204, None)
        if parsed.path.startswith("/api/v0/searches/"):
            sid = parsed.path.rsplit("/", 1)[1]
            with LOCK:
                SEARCHES.pop(sid, None)
            return self._json(200, {"ok": True})
        return self._json(404, {"error": "not found"})


def main():
    os.makedirs(DOWNLOADS_DIR, exist_ok=True)
    for peer in PEERS.values():
        out = os.path.join(DOWNLOADS_DIR, peer["local"])
        if not os.path.exists(out):
            generate(peer, out)
            print(f"[fake-slskd] generated {out}")
    # The janitor reclaims unreferenced staging files and the pipeline's
    # terminal discard removes the file when an item finishes with it, so a
    # fixture staged only at startup disappears mid-suite. main() also primes
    # the shared volume so the healthcheck's file existence is meaningful.
    server = ThreadingHTTPServer(("0.0.0.0", 5030), Handler)
    print("[fake-slskd] listening on :5030")
    server.serve_forever()


if __name__ == "__main__":
    main()
