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

fake_bin="$tmp/bin"
mkdir -p "$fake_bin"
original_path="$PATH"

cat >"$fake_bin/docker" <<'EOF_DOCKER'
#!/usr/bin/env bash
if [[ "$1" == "compose" && "$2" == "version" ]]; then
  echo "Docker Compose version v2.test"
  exit 0
fi
if [[ "$1" == "info" ]]; then
  exit 0
fi
exit 1
EOF_DOCKER
chmod +x "$fake_bin/docker"
PATH="$fake_bin:$original_path"
docker_compose_available
docker_daemon_available

cat >"$fake_bin/docker" <<'EOF_DOCKER_NO_COMPOSE'
#!/usr/bin/env bash
if [[ "$1" == "compose" && "$2" == "version" ]]; then
  exit 1
fi
if [[ "$1" == "info" ]]; then
  exit 0
fi
exit 1
EOF_DOCKER_NO_COMPOSE
cat >"$fake_bin/docker-compose" <<'EOF_LEGACY_COMPOSE'
#!/usr/bin/env bash
if [[ "$1" == "version" ]]; then
  echo "docker-compose version 1.test"
  exit 0
fi
exit 1
EOF_LEGACY_COMPOSE
chmod +x "$fake_bin/docker" "$fake_bin/docker-compose"
docker_compose_available

cat >"$fake_bin/dpkg-query" <<'EOF_DPKG'
#!/usr/bin/env bash
exit 1
EOF_DPKG
cat >"$fake_bin/apt-cache" <<'EOF_APT_CACHE'
#!/usr/bin/env bash
if [[ "$1" == "show" && "$2" == "docker-compose-v2" ]]; then
  echo "Package: docker-compose-v2"
  exit 0
fi
exit 100
EOF_APT_CACHE
chmod +x "$fake_bin/dpkg-query" "$fake_bin/apt-cache"
[[ "$(select_docker_compose_package)" == "docker-compose-v2" ]]

PATH="$original_path"

echo "install.sh parser/idempotency tests: OK"
