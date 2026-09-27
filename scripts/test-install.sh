#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=install.sh
source "$ROOT/install.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
file="$tmp/devbox.env"

upsert_env_file "$file" DEVBOX_HTTP_ADDR 127.0.0.1:8787
upsert_env_file "$file" DEVBOX_HTTP_ADDR 127.0.0.1:8787
[[ "$(grep -c '^DEVBOX_HTTP_ADDR=' "$file")" -eq 1 ]]
[[ "$(cat "$file")" == 'DEVBOX_HTTP_ADDR=127.0.0.1:8787' ]]

parse_args --uninstall --purge
[[ "$MODE" == '--uninstall' ]]
[[ "$PURGE" -eq 1 ]]

if parse_args --install --status; then
  echo "parser accepted conflicting modes" >&2
  exit 1
fi

echo "install.sh parser/idempotency tests: OK"
