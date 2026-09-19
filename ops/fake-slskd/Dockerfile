FROM python:3.12-alpine
# E2E-only slskd stand-in: ffmpeg generates the fixture FLACs into the shared
# staging volume; the API stand-in is pure stdlib.
RUN apk add --no-cache ffmpeg
COPY entrypoint.py /entrypoint.py
EXPOSE 5030
ENTRYPOINT ["python3", "/entrypoint.py"]
