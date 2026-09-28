#!/usr/bin/env bash
set -Eeuo pipefail

ENV_FILE="${DEVBOX_ENV_FILE:-/etc/devbox/devbox.env}"
LOCK_FILE="${DEVBOX_UPDATE_LOCK:-/run/lock/devbox-update.lock}"
LOG_FILE="${DEVBOX_UPDATE_LOG:-/var/log/devbox-update.log}"
SOURCE_REPOSITORY="${DEVBOX_SOURCE_REPOSITORY:-https://github.com/chmajster/DevBox-Uniwersal.git}"
SOURCE_REF="${DEVBOX_SOURCE_REF:-main}"
TMP_DIR=""

log() {
  printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >>"$LOG_FILE"
}

cleanup() {
  [[ -z "$TMP_DIR" ]] || rm -rf "$TMP_DIR"
}
trap cleanup EXIT

if [[ -r "$ENV_FILE" ]]; then
  # Only read the updater-specific values and installed version; never source arbitrary shell.
  while IFS='=' read -r key value; do
    case "$key" in
      DEVBOX_UPDATE_REPOSITORY) SOURCE_REPOSITORY="$value" ;;
      DEVBOX_UPDATE_REF) SOURCE_REF="$value" ;;
      DEVBOX_VERSION) CURRENT_VERSION="$value" ;;
    esac
  done <"$ENV_FILE"
fi

CURRENT_VERSION="${CURRENT_VERSION:-unknown}"
mkdir -p "$(dirname "$LOCK_FILE")" "$(dirname "$LOG_FILE")"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  log "Aktualizacja już trwa; pomijam równoległe uruchomienie."
  exit 0
fi

command -v git >/dev/null 2>&1 || { log "Brak git."; exit 1; }
TMP_DIR="$(mktemp -d)"
log "Sprawdzam $SOURCE_REPOSITORY ref=$SOURCE_REF current=$CURRENT_VERSION"

git clone --depth 1 --branch "$SOURCE_REF" "$SOURCE_REPOSITORY" "$TMP_DIR/source" >>"$LOG_FILE" 2>&1
NEW_VERSION="$(git -C "$TMP_DIR/source" rev-parse HEAD)"

if [[ "$CURRENT_VERSION" == "$NEW_VERSION" ]]; then
  log "Brak aktualizacji. Wersja $NEW_VERSION jest aktualna."
  exit 0
fi

[[ -f "$TMP_DIR/source/install.sh" ]] || { log "Brak install.sh w źródłach."; exit 1; }
[[ -f "$TMP_DIR/source/backend/go.mod" ]] || { log "Brak backend/go.mod w źródłach."; exit 1; }
[[ -f "$TMP_DIR/source/frontend/package-lock.json" ]] || { log "Brak frontend/package-lock.json w źródłach."; exit 1; }

log "Aktualizuję $CURRENT_VERSION -> $NEW_VERSION"
DEVBOX_SOURCE_REPOSITORY="$SOURCE_REPOSITORY" DEVBOX_SOURCE_REF="$SOURCE_REF" DEVBOX_USE_CURRENT_SOURCE=1 bash "$TMP_DIR/source/install.sh" --update >>"$LOG_FILE" 2>&1

if command -v systemctl >/dev/null 2>&1; then
  systemctl restart devbox.service >>"$LOG_FILE" 2>&1
fi
log "Aktualizacja do $NEW_VERSION zakończona."
