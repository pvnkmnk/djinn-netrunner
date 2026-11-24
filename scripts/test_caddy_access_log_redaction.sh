#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 2; }
command -v curl >/dev/null || { echo "curl is required" >&2; exit 2; }

caddyfile_contents="$(<"$repo_root/ops/caddy/Caddyfile")"
if [[ "$caddyfile_contents" != *'import ./access-log-format.caddy'* || \
      "$caddyfile_contents" != *'import subsonic_access_log_format'* ]]; then
    echo "Production Caddyfile is not using the tested access-log filter snippet" >&2
    exit 1
fi

if [[ -n "${CADDY_BIN:-}" ]]; then
    caddy_bin="$CADDY_BIN"
elif command -v caddy >/dev/null 2>&1; then
    caddy_bin="$(command -v caddy)"
else
    caddy_bin=""
fi
use_docker=0
if [[ -z "$caddy_bin" ]]; then
    if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
        use_docker=1
    else
        echo "Caddy or a working Docker daemon is required for this integration test" >&2
        exit 2
    fi
fi

work_dir="$(mktemp -d)"
chmod 755 "$work_dir"
log_file="$work_dir/caddy.log"
backend_log="$work_dir/backend.log"
backend_pid=""
caddy_pid=""
container_name="caddy-log-redaction-$$"

cleanup() {
    set +e
    if [[ -n "$container_name" ]] && command -v docker >/dev/null 2>&1; then
        docker rm -f "$container_name" >/dev/null 2>&1
    fi
    if [[ -n "$caddy_pid" ]]; then
        kill "$caddy_pid" >/dev/null 2>&1
        wait "$caddy_pid" >/dev/null 2>&1
    fi
    if [[ -n "$backend_pid" ]]; then
        kill "$backend_pid" >/dev/null 2>&1
        wait "$backend_pid" >/dev/null 2>&1
    fi
    rm -rf "$work_dir"
}
trap cleanup EXIT

read -r backend_port caddy_port < <(python3 - <<'PY'
import socket
ports = []
for _ in range(2):
    sock = socket.socket()
    sock.bind(("127.0.0.1", 0))
    ports.append(sock.getsockname()[1])
    sock.close()
print(*ports)
PY
)

python3 - "$backend_port" >"$backend_log" 2>&1 <<'PY' &
from http.server import BaseHTTPRequestHandler, HTTPServer
import sys

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"ok")

    def log_message(self, *_args):
        pass

HTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
PY
backend_pid=$!

cp "$repo_root/ops/caddy/access-log-format.caddy" "$work_dir/access-log-format.caddy"
cat >"$work_dir/Caddyfile" <<EOF
{
    admin off
    auto_https off
}

import ./access-log-format.caddy

:$caddy_port {
    log {
        output stdout
        import subsonic_access_log_format
    }
    reverse_proxy 127.0.0.1:$backend_port
}
EOF
chmod 644 "$work_dir/Caddyfile" "$work_dir/access-log-format.caddy"

if [[ "$use_docker" -eq 1 ]]; then
    docker run --rm --name "$container_name" --network host \
        -v "$work_dir:$work_dir:ro" \
        caddy:2-alpine run --config "$work_dir/Caddyfile" --adapter caddyfile \
        >"$log_file" 2>&1 &
    caddy_pid=$!
else
    "$caddy_bin" run --config "$work_dir/Caddyfile" --adapter caddyfile \
        >"$log_file" 2>&1 &
    caddy_pid=$!
fi

ready=0
for _ in $(seq 1 60); do
    if ! kill -0 "$caddy_pid" >/dev/null 2>&1; then
        echo "Caddy exited before the test listener became ready" >&2
        cat "$log_file" >&2
        exit 1
    fi
    if python3 - "$caddy_port" <<'PY'
import socket
import sys
try:
    with socket.create_connection(("127.0.0.1", int(sys.argv[1])), timeout=0.2):
        pass
except OSError:
    raise SystemExit(1)
PY
    then
        ready=1
        break
    fi
    sleep 0.1
done
if [[ "$ready" -ne 1 ]]; then
    echo "Caddy did not start the test listener" >&2
    cat "$log_file" >&2
    exit 1
fi

secret_p="P_SECRET_696_TEST"
secret_t="T_SECRET_696_TEST"
secret_s="S_SECRET_696_TEST"
request_url="http://127.0.0.1:$caddy_port/rest/ping.view?u=smoke%40example.test&p=$secret_p&t=$secret_t&s=$secret_s&c=redaction-test&v=1.16.1"
status="$(curl --silent --output /dev/null --write-out '%{http_code}' "$request_url" 2>/dev/null || true)"
if [[ "$status" != "200" ]]; then
    echo "Test request did not receive HTTP 200" >&2
    exit 1
fi

python3 - "$log_file" "$secret_p" "$secret_t" "$secret_s" <<'PY'
import json
import sys
import time
from urllib.parse import parse_qs, urlsplit

log_path, *secrets = sys.argv[1:]
text = ""
entry = None
for _ in range(60):
    try:
        with open(log_path, encoding="utf-8") as handle:
            text = handle.read()
    except FileNotFoundError:
        pass
    if any(secret in text for secret in secrets):
        raise SystemExit("A credential marker appeared in the Caddy log")
    for line in text.splitlines():
        try:
            record = json.loads(line)
        except json.JSONDecodeError:
            continue
        request = record.get("request") or {}
        uri = request.get("uri", "")
        if record.get("msg") == "handled request" and "c=redaction-test" in uri:
            entry = record
            break
    if entry is not None:
        break
    time.sleep(0.1)

if entry is None:
    raise SystemExit("No matching Caddy access-log entry was produced")
request = entry.get("request") or {}
uri = request.get("uri", "")
query = parse_qs(urlsplit(uri).query, keep_blank_values=True)
if any(key in query for key in ("p", "t", "s")):
    raise SystemExit("A credential query parameter remained in the Caddy access log")
if query.get("c") != ["redaction-test"] or query.get("v") != ["1.16.1"]:
    raise SystemExit("Safe query metadata was not preserved in the Caddy access log")
if request.get("method") != "GET" or entry.get("status") != 200:
    raise SystemExit("Safe method/status metadata was not preserved in the Caddy access log")
print("Caddy access-log redaction passed: p/t/s absent; c/v, method, and status preserved.")
PY
