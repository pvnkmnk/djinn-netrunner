#!/bin/sh
# Entry point: start each probe role from its own file. Role scripts live
# beside this entrypoint (wrong-work.sh, success.sh, flip-server.py) — one
# role per file, per the audit's QUALITY finding on the monolithic
# entrypoint.
#
# Process model: the wrong-work role owns the foreground (busybox httpd on
# :8080 is the compose healthcheck target — the container is "ready" exactly
# when it answers); the flip server runs in the background on :8081.
set -eu

# Success-path source: generate once at startup so the file exists before
# any spec seeds a success probe.
/success.sh

# Multi-hop role: background python server on :8081.
python3 /flip-server.py &
echo 'flip probe listening on :8081 (Range: bytes=0-0 -> clean 200; anything else -> 302 -> RFC1918)'

# Wrong-work role: generate the decoy, then serve /srv/audio on :8080 in
# the foreground (healthcheck target).
exec /wrong-work.sh
