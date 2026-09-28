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

mysql_env="$tmp/mysql.env"
ENV_FILE="$mysql_env"
LOG_FILE="$tmp/install.log"
unset DEVBOX_MYSQL_MANAGED || true
mysql_managed_mode

upsert_env_file "$mysql_env" DEVBOX_MYSQL_ADMIN_PASSWORD legacy-secret
if mysql_managed_mode; then
  echo "legacy MySQL installation was incorrectly switched to managed mode" >&2
  exit 1
fi

DEVBOX_MYSQL_MANAGED=true
mysql_managed_mode
unset DEVBOX_MYSQL_MANAGED
remove_env_key "$mysql_env" DEVBOX_MYSQL_ADMIN_PASSWORD
mysql_managed_mode

DEVBOX_UPDATE_PROGRESS_FILE="$tmp/update-status"
DEVBOX_UPDATE_CURRENT_VERSION="old-version"
DEVBOX_UPDATE_TARGET_VERSION="new-version"
DEVBOX_UPDATE_STARTED_AT="2026-09-28T20:00:00Z"
stage 3 "Build backendu i helpera"
grep -q '^STATE=runningfake_bin="$tmp/bin"
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
if [[ "$1" == "show" ]]; then
  case "$2" in
    docker-compose-v2|default-mysql-client|default-mysql-server)
      echo "Package: $2"
      exit 0
      ;;
  esac
fi
exit 100
EOF_APT_CACHE
chmod +x "$fake_bin/dpkg-query" "$fake_bin/apt-cache"
[[ "$(select_docker_compose_package)" == "docker-compose-v2" ]]
[[ "$(select_mysql_client_package)" == "default-mysql-client" ]]
[[ "$(select_mysql_server_package)" == "default-mysql-server" ]]

PATH="$original_path"

echo "install.sh parser/idempotency tests: OK"
 "$DEVBOX_UPDATE_PROGRESS_FILE"
grep -q '^PERCENT=58fake_bin="$tmp/bin"
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
if [[ "$1" == "show" ]]; then
  case "$2" in
    docker-compose-v2|default-mysql-client|default-mysql-server)
      echo "Package: $2"
      exit 0
      ;;
  esac
fi
exit 100
EOF_APT_CACHE
chmod +x "$fake_bin/dpkg-query" "$fake_bin/apt-cache"
[[ "$(select_docker_compose_package)" == "docker-compose-v2" ]]
[[ "$(select_mysql_client_package)" == "default-mysql-client" ]]
[[ "$(select_mysql_server_package)" == "default-mysql-server" ]]

PATH="$original_path"

echo "install.sh parser/idempotency tests: OK"
 "$DEVBOX_UPDATE_PROGRESS_FILE"
grep -q '^STAGE=backendfake_bin="$tmp/bin"
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
if [[ "$1" == "show" ]]; then
  case "$2" in
    docker-compose-v2|default-mysql-client|default-mysql-server)
      echo "Package: $2"
      exit 0
      ;;
  esac
fi
exit 100
EOF_APT_CACHE
chmod +x "$fake_bin/dpkg-query" "$fake_bin/apt-cache"
[[ "$(select_docker_compose_package)" == "docker-compose-v2" ]]
[[ "$(select_mysql_client_package)" == "default-mysql-client" ]]
[[ "$(select_mysql_server_package)" == "default-mysql-server" ]]

PATH="$original_path"

echo "install.sh parser/idempotency tests: OK"
 "$DEVBOX_UPDATE_PROGRESS_FILE"
grep -q '^TARGET_VERSION=new-versionfake_bin="$tmp/bin"
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
if [[ "$1" == "show" ]]; then
  case "$2" in
    docker-compose-v2|default-mysql-client|default-mysql-server)
      echo "Package: $2"
      exit 0
      ;;
  esac
fi
exit 100
EOF_APT_CACHE
chmod +x "$fake_bin/dpkg-query" "$fake_bin/apt-cache"
[[ "$(select_docker_compose_package)" == "docker-compose-v2" ]]
[[ "$(select_mysql_client_package)" == "default-mysql-client" ]]
[[ "$(select_mysql_server_package)" == "default-mysql-server" ]]

PATH="$original_path"

echo "install.sh parser/idempotency tests: OK"
 "$DEVBOX_UPDATE_PROGRESS_FILE"
unset DEVBOX_UPDATE_PROGRESS_FILE DEVBOX_UPDATE_CURRENT_VERSION DEVBOX_UPDATE_TARGET_VERSION DEVBOX_UPDATE_STARTED_AT

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
if [[ "$1" == "show" ]]; then
  case "$2" in
    docker-compose-v2|default-mysql-client|default-mysql-server)
      echo "Package: $2"
      exit 0
      ;;
  esac
fi
exit 100
EOF_APT_CACHE
chmod +x "$fake_bin/dpkg-query" "$fake_bin/apt-cache"
[[ "$(select_docker_compose_package)" == "docker-compose-v2" ]]
[[ "$(select_mysql_client_package)" == "default-mysql-client" ]]
[[ "$(select_mysql_server_package)" == "default-mysql-server" ]]

PATH="$original_path"

echo "install.sh parser/idempotency tests: OK"
