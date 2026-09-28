#!/usr/bin/env bash
set -Eeuo pipefail

ENV_FILE="${DEVBOX_ENV_FILE:-/etc/devbox/devbox.env}"
LOCK_FILE="${DEVBOX_UPDATE_LOCK:-/run/lock/devbox-update.lock}"
LOG_FILE="${DEVBOX_UPDATE_LOG:-/var/log/devbox-update.log}"
PROGRESS_FILE="${DEVBOX_UPDATE_PROGRESS_FILE:-/var/lib/devbox/update-status}"
SOURCE_REPOSITORY="${DEVBOX_SOURCE_REPOSITORY:-https://github.com/chmajster/DevBox-Uniwersal.git}"
SOURCE_REF="${DEVBOX_SOURCE_REF:-main}"
TMP_DIR=""
PROGRESS_PERCENT=0
PROGRESS_STAGE="idle"
TARGET_VERSION=""
STARTED_AT=""

log() {
  printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >>"$LOG_FILE"
}

safe_value() {
  local value="${1:-}"
  value="${value//$'\n'/ }"
  value="${value//$'\r'/ }"
  printf '%s' "$value"
}

write_progress() {
  local state="$1"
  local percent="$2"
  local stage="$3"
  local message="$4"
  local error_message="${5:-}"
  local now finished_at="" tmp

  PROGRESS_PERCENT="$percent"
  PROGRESS_STAGE="$stage"
  now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  case "$state" in
    succeeded|failed|no_update) finished_at="$now" ;;
  esac

  mkdir -p "$(dirname "$PROGRESS_FILE")" 2>/dev/null || true
  tmp="${PROGRESS_FILE}.tmp.$$"
  {
    printf 'STATE=%s\n' "$(safe_value "$state")"
    printf 'PERCENT=%s\n' "$percent"
    printf 'STAGE=%s\n' "$(safe_value "$stage")"
    printf 'MESSAGE=%s\n' "$(safe_value "$message")"
    printf 'CURRENT_VERSION=%s\n' "$(safe_value "${CURRENT_VERSION:-unknown}")"
    printf 'TARGET_VERSION=%s\n' "$(safe_value "$TARGET_VERSION")"
    printf 'STARTED_AT=%s\n' "$(safe_value "$STARTED_AT")"
    printf 'UPDATED_AT=%s\n' "$now"
    printf 'FINISHED_AT=%s\n' "$finished_at"
    printf 'ERROR=%s\n' "$(safe_value "$error_message")"
  } >"$tmp" 2>/dev/null && chmod 0644 "$tmp" 2>/dev/null && mv -f "$tmp" "$PROGRESS_FILE" 2>/dev/null || {
    rm -f "$tmp" 2>/dev/null || true
    true
  }
}

cleanup() {
  [[ -z "$TMP_DIR" ]] || rm -rf "$TMP_DIR"
}

on_error() {
  local rc="$1"
  trap - ERR
  local message="Aktualizacja nie powiodła się na etapie: $PROGRESS_STAGE."
  log "$message"
  write_progress "failed" "$PROGRESS_PERCENT" "$PROGRESS_STAGE" "$message" "Sprawdź $LOG_FILE."
  exit "$rc"
}

fail_update() {
  local message="$1"
  log "$message"
  write_progress "failed" "$PROGRESS_PERCENT" "$PROGRESS_STAGE" "$message" "$message"
  exit 1
}

trap cleanup EXIT
trap 'on_error $?' ERR

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

STARTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
write_progress "running" 2 "starting" "Uruchamianie procesu aktualizacji."

write_progress "running" 8 "source_check" "Sprawdzanie narzędzi i źródła aktualizacji."
command -v git >/dev/null 2>&1 || fail_update "Brak git."

TMP_DIR="$(mktemp -d)"
log "Sprawdzam $SOURCE_REPOSITORY ref=$SOURCE_REF current=$CURRENT_VERSION"

write_progress "running" 15 "download" "Pobieranie najnowszej wersji z repozytorium Git."
git clone --depth 1 --branch "$SOURCE_REF" "$SOURCE_REPOSITORY" "$TMP_DIR/source" >>"$LOG_FILE" 2>&1
NEW_VERSION="$(git -C "$TMP_DIR/source" rev-parse HEAD)"
TARGET_VERSION="$NEW_VERSION"
write_progress "running" 25 "download" "Źródła zostały pobrane. Porównywanie wersji."

if [[ "$CURRENT_VERSION" == "$NEW_VERSION" ]]; then
  log "Brak aktualizacji. Wersja $NEW_VERSION jest aktualna."
  write_progress "no_update" 100 "completed" "Brak aktualizacji. Zainstalowana wersja jest aktualna."
  exit 0
fi

write_progress "running" 30 "validation" "Walidacja pobranych źródeł przed instalacją."
[[ -f "$TMP_DIR/source/install.sh" ]] || fail_update "Brak install.sh w źródłach."
[[ -f "$TMP_DIR/source/backend/go.mod" ]] || fail_update "Brak backend/go.mod w źródłach."
[[ -f "$TMP_DIR/source/frontend/package-lock.json" ]] || fail_update "Brak frontend/package-lock.json w źródłach."

log "Aktualizuję $CURRENT_VERSION -> $NEW_VERSION"
write_progress "running" 34 "environment" "Uruchamianie instalatora aktualizacji."
DEVBOX_SOURCE_REPOSITORY="$SOURCE_REPOSITORY" \
DEVBOX_SOURCE_REF="$SOURCE_REF" \
DEVBOX_USE_CURRENT_SOURCE=1 \
DEVBOX_UPDATE_PROGRESS_FILE="$PROGRESS_FILE" \
DEVBOX_UPDATE_STARTED_AT="$STARTED_AT" \
DEVBOX_UPDATE_CURRENT_VERSION="$CURRENT_VERSION" \
DEVBOX_UPDATE_TARGET_VERSION="$NEW_VERSION" \
bash "$TMP_DIR/source/install.sh" --update >>"$LOG_FILE" 2>&1

write_progress "running" 99 "restart" "Restart usługi DevBox po poprawnej instalacji."
if command -v systemctl >/dev/null 2>&1; then
  systemctl restart devbox.service >>"$LOG_FILE" 2>&1
fi
log "Aktualizacja do $NEW_VERSION zakończona."
CURRENT_VERSION="$NEW_VERSION"
write_progress "succeeded" 100 "completed" "Aktualizacja zakończona powodzeniem."
