#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ -z "${DEVBOX_BOOTSTRAP_ADMIN_USERNAME:-}" || -z "${DEVBOX_BOOTSTRAP_ADMIN_PASSWORD:-}" ]]; then
  echo "DEVBOX_BOOTSTRAP_ADMIN_USERNAME and DEVBOX_BOOTSTRAP_ADMIN_PASSWORD are required for first login." >&2
  exit 1
fi

cleanup() {
  if [[ -n "${BACKEND_PID:-}" ]]; then
    kill "$BACKEND_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

( cd backend && DEVBOX_MIGRATIONS_DIR="$ROOT/migrations" go run ./cmd/devbox ) &
BACKEND_PID=$!
( cd frontend && npm run dev )
