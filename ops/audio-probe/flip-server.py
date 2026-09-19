#!/usr/bin/env python3
"""Role: flip server (the multi-hop post-handover refusal probe, DJI-500's
documented residual — closed live in PR #259).

The rule is deterministic per request, so worker retries see the same
behavior the first attempt did:

  * a request carrying the guard's `Range: bytes=0-0` signature (the
    pre-handover walk is the ONLY caller that sends it) gets a clean 200 —
    the walk sees a public destination and hands the URL over;
  * every other request (yt-dlp's actual fetch after handover) gets a 302
    to an RFC1918 address — the hop the downloader performs itself, which
    the pre-flight cannot follow, so the egress boundary must deny it.

A counter was tried first and was wrong in an instructive way: attempt 1
behaves correctly, but its proxy denial is a retryable failure, and attempt
2's walk then saw the redirect and ITS refusal became the terminal wording —
the run recorded the pre-flight refusal instead of the post-handover one.
"""
import socket
import socketserver
from http.server import BaseHTTPRequestHandler

AUDIO = '/srv/audio/wrong-work.flac'


class FlipHandler(BaseHTTPRequestHandler):
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


# Dual-stack bind: the probe's name resolves to the documentation v6
# address, so the flip server must accept v6 connections too (an AF_INET
# bind would listen only on v4 and refuse every request arriving over the
# doc net).
class DualStackServer(socketserver.TCPServer):
    address_family = socket.AF_INET6


if __name__ == '__main__':
    DualStackServer(('::', 8081), FlipHandler).serve_forever()
