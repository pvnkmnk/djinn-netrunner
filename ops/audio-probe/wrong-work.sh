#!/bin/sh
# Role: wrong-work server (GA gap A1 / the Soulseek + yt-dlp wrong-work
# refusal probes). Generates a real, playable FLAC whose tags name a
# DIFFERENT work than the probe item requests, then serves /srv/audio
# statically on :8080 via busybox httpd.
#
# The probe's whole point is that the download SUCCEEDS — real playable
# audio reaches the import gate — and the identity gate (download_gate.go)
# is the layer that refuses it. Nothing else in the chain may be the
# refuser: not the pre-handover walk (DJI-500), not the proxy (DJI-501),
# not plausibility. Hence: a genuinely playable file, correctly sized,
# whose tags just disagree with the request.
set -eu

OUT=/srv/audio/wrong-work.flac
# The decoy names must share NO word with the requests the specs seed
# ("Wrong Work Probe"/"Unrelated Record" and "Verge Ensemble"/"Null Hour"):
# the identity gate folds names to words and treats ANY shared word as "not
# confidently a different work" (a whole-word overlap is exactly what legit
# name variants look like). The first live run proved that property the hard
# way — a decoy sharing "Probe" sailed through.
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

# Compose healthcheck target: the container is ready exactly when httpd
# answers. Without it the worker could claim the probe job while ffmpeg is
# still writing the FLAC, and the spec's download would 404.
cd /srv/audio
exec httpd -f -p 8080
