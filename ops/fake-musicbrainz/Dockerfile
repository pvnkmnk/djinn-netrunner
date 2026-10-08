FROM python:3.12-alpine
# E2E-only MusicBrainz stand-in, pure stdlib. Mirrors ops/fake-slskd: the point
# is a fixed answer on every run, so nothing here reads a clock, the network, or
# a fixture from disk.
#
# See entrypoint.py for why the seam exists (DJI-603) and what it serves.
COPY entrypoint.py /entrypoint.py
EXPOSE 8082
ENTRYPOINT ["python3", "/entrypoint.py"]