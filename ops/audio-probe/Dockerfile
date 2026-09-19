FROM alpine:3.21
# E2E-only image: generates probe audio at startup and serves it.
# ffmpeg only for the generation; httpd (busybox-extras — not in alpine's
# trimmed busybox) serves it; python runs the multi-hop flip server.
RUN apk add --no-cache ffmpeg busybox-extras python3
COPY entrypoint.sh wrong-work.sh success.sh flip-server.py /
RUN chmod +x /entrypoint.sh /wrong-work.sh /success.sh /flip-server.py && mkdir -p /srv/audio
EXPOSE 8080 8081
ENTRYPOINT ["/entrypoint.sh"]
