#!/bin/sh
# Role: success-path source for the HTTP-fetch entrance (GA gap C10's
# boundary companion). Generates the FLAC whose tags MATCH the success
# probe's request, so a download that arrives over HTTP (the fallback
# entrance / the boundary's allowlist path) passes the identity gate and
# imports. Served by the same :8080 httpd as the wrong-work decoy: one
# static server, one port, two files.
#
# Today's success SPEC rides the Soulseek entrance (fake-slskd's clean peer
# generates its own file); this fixture exists for clauses that need the
# HTTP entrance to deliver a matching work: point the seed payload's url
# at http://audio-probe.e2e.test:8080/clean-success.flac.
#
# The comment tag is unique per generation for the same reason fake-slskd's
# fixture is: deterministic bytes hash identically to a copy an earlier run
# imported, and the hash-duplicate path would short-circuit the identity
# gate this probe exists to exercise.
set -eu

TAG_ARTIST="Clean Success Artist"
TAG_ALBUM="Clean Success Album"
TAG_TITLE="Clean Success Song"

ffmpeg -y -loglevel error -f lavfi -i "sine=frequency=440:duration=20" \
  -metadata "artist=${TAG_ARTIST}" \
  -metadata "album_artist=${TAG_ARTIST}" \
  -metadata "album=${TAG_ALBUM}" \
  -metadata "title=${TAG_TITLE}" \
  -metadata "comment=$(date +%s%N | md5sum | cut -c1-32)" \
  -c:a flac /srv/audio/clean-success.flac

echo "serving clean-success.flac (tags: ${TAG_ARTIST} / ${TAG_ALBUM}) on :8080"
