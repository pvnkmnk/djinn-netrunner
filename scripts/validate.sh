#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_DIR="$REPO_ROOT/backend"

echo "[validate] backend dir: $BACKEND_DIR"
cd "$BACKEND_DIR"

echo "[validate] go vet ./..."
go vet ./...

echo "[validate] go test ./..."
go test ./...

echo "[validate] go build ./cmd/server ./cmd/worker ./cmd/cli ./cmd/agent"
go build ./cmd/server ./cmd/worker ./cmd/cli ./cmd/agent

if command -v govulncheck >/dev/null 2>&1; then
  echo "[validate] govulncheck ./..."
  govulncheck ./...
else
  echo "[validate] govulncheck not installed; skipping"
  echo "[validate] install with: go install golang.org/x/vuln/cmd/govulncheck@latest"
fi

echo "[validate] success"
