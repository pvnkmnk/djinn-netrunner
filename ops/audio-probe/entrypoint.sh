#!/bin/sh
# E2E-only: the wrong-work refusal probe's audio source (DJI-499 follow-up /
# GA gap A1). Generates a real, playable FLAC whose tags name a DIFFERENT work
# than the probe item requests, then serves it on port 8080.
#
# The probe's whole point is that the download SUCCEEDS — real playable audio
# reaches the import gate — and the identity gate (rejectUnusableDownload,
# download_gate.go) is the layer that refuses it. Nothing else in the chain may
# be the refuser: not the pre-handover walk (DJI-500), not the proxy (DJI-501),
# not plausibility. Hence: a genuinely playable file, correctly sized, whose
# tags just disagree with the request.
#
# /flip (port 8081) is the multi-hop probe. The rule is deterministic per
# request, so worker retries see the same behavior the first attempt did:
#
#   * a request carrying the guard's `Range: bytes=0-0` signature (the
#     pre-handover walk is the ONLY caller that sends it) gets a clean 200 —
#     the walk sees a public destination and hands the URL over;
#   * every other request (yt-dlp's actual fetch after handover) gets a 302
#     to an RFC1918 address — the hop the downloader performs itself, which
#     the pre-flight cannot follow, so the egress boundary must deny it.
#
# A counter was tried first and was wrong in an instructive way: attempt 1
# behaves correctly, but its proxy denial is a retryable failure, and attempt
# 2's walk then saw the redirect and ITS refusal became the terminal wording —
# the run recorded the pre-flight refusal instead of the post-handover one.
set -eu

OUT=/srv/audio/wrong-work.flac
# The decoy names must share NO word with the request the spec seeds ("Wrong
# Work Probe" / "DJI Gate Proof"): the identity gate folds names to words and
# treats ANY shared word as "not confidently a different work" (a whole-word
# overlap is exactly what legit name variants look like). The first live run
# proved that property the hard way — a decoy sharing "Probe" sailed through.
TAG_ARTIST="Totally Different Band"
TAG_ALBUM="Unrelated Record"
TAG_TITLE="Some Other Song"

# ffmpeg lavfi: a 20-second 440 Hz sine is a real FLAC every probe accepts.
ffmpeg -y -loglevel error -f lavfi -i "sine=frequency=440:duration=20" \
  -metadata "artist=${TAG_ARTIST}" \
  -metadata "album_artist=${TAG_ARTIST}" \
  -metadata "album=${TAG_ALBUM}" \
  -metadata "title=${TAG_TITLE}" \
  -c:a flac "$OUT"

echo "serving ${OUT} (tags: ${TAG_ARTIST} / ${TAG_ALBUM}) on :8080"

# busybox httpd serves /srv/audio on 8080 (plain static files for the
# wrong-work probe); the flip server below runs python for /flip and /reset.
cat > /tmp/flip-server.py <<'PYEOF'
import os
import socketserver
from http.server import BaseHTTPRequestHandler

COUNT_FILE = '/tmp/flip-count'
AUDIO = '/srv/audio/wrong-work.flac'

class H(BaseHTTPRequestHandler):
    def _deny(self):
        # RFC 1918 destination: the boundary's deny list refuses the
        # CONNECT yt-dlp makes to follow this hop.
        self.send_response(302)
        self.send_header('Location', 'http://192.168.255.10/hop.flac')
        self.send_header('Content-Length', '0')
        self.end_headers()

    def _serve(self, head=False):
        rng = self.headers.get('Range', '')
        if rng.startswith('bytes=0-'):
            # The guard's walk: it asks for bytes=0-0 and never reads the
            # body. A clean public answer is what hands the URL over.
            self.send_response(200)
            self.send_header('Content-Type', 'audio/flac')
            self.send_header('Content-Length', '1')
            self.send_header('Accept-Ranges', 'bytes')
            self.end_headers()
            if not head:
                self.wfile.write(open(AUDIO, 'rb').read(1))
            return
        self._deny()

    def do_GET(self):
        self._serve()

    def do_HEAD(self):
        # The pre-flight walk may probe with HEAD; same rule.
        self._serve(head=True)

    def log_message(self, fmt, *args):
        print('[flip]', self.address_string(), fmt % args)

socketserver.TCPServer.allow_reuse_address = True

# Dual-stack bind: the probe's name resolves to the documentation v6 address,
# so the flip server must accept v6 connections too (an AF_INET bind would
# listen only on v4 and refuse every request that arrives over the doc net).
import socket

class DualStackServer(socketserver.TCPServer):
    address_family = socket.AF_INET6

DualStackServer(('::', 8081), H).serve_forever()
PYEOF

echo 'flip probe listening on :8081 (Range: bytes=0-0 -> clean 200; anything else -> 302 -> RFC1918)'
python3 /tmp/flip-server.py &

# Compose healthcheck target: the container is ready exactly when this answers.
# Without it the worker could claim the probe job while ffmpeg is still writing
# the FLAC, and the spec's download would 404.
cd /srv/audio
exec httpd -f -p 8080
