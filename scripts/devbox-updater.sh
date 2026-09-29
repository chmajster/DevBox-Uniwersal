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
  if ! {
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
  } >"$tmp" 2>/dev/null; then
    rm -f "$tmp" 2>/dev/null || true
    return 0
  fi
  if ! chmod 0644 "$tmp" 2>/dev/null; then
    rm -f "$tmp" 2>/dev/null || true
    return 0
  fi
  if ! mv -f "$tmp" "$PROGRESS_FILE" 2>/dev/null; then
    rm -f "$tmp" 2>/dev/null || true
  fi
}

# The complete main function is parsed before installation can replace this file.
# Public release metadata is signed; rollback snapshots are encrypted with the
# existing master key. No key, token or environment file is written to logs.
INSTALL_ROOT="${DEVBOX_INSTALL_ROOT:-/opt/devbox}"
LIBEXEC_DIR="${DEVBOX_LIBEXEC_DIR:-/usr/local/lib/devbox}"
BIN_LINK="${DEVBOX_BIN_LINK:-/usr/local/bin/devbox}"
DATA_DIR="${DEVBOX_DATA_DIR:-/var/lib/devbox}"
BACKUP_ROOT="${DEVBOX_UPDATE_BACKUP_ROOT:-/var/backups/devbox-updates}"
UPDATE_MODE="${DEVBOX_UPDATE_MODE:-signed}"
ALLOW_UNSIGNED="${DEVBOX_UPDATE_ALLOW_UNSIGNED:-false}"
RELEASE_BASE="${DEVBOX_UPDATE_RELEASE_BASE:-https://github.com/chmajster/DevBox-Uniwersal/releases/latest/download}"
PUBLIC_KEY="${DEVBOX_UPDATE_PUBLIC_KEY:-/etc/devbox/update-public.key}"
DATABASE_PATH="$DATA_DIR/devbox.db"
HTTP_ADDR="127.0.0.1:8787"
MIGRATIONS_DIR="$INSTALL_ROOT/migrations"
FRONTEND_DIR="$INSTALL_ROOT/frontend"
NGINX_AVAILABLE="$DATA_DIR/nginx/sites-available"
NGINX_ENABLED="$DATA_DIR/nginx/sites-enabled"
ROLLBACK_KEY=""
LAST_RELEASE_AT=""
BACKUP_DIR=""
SNAPSHOT_CREATED=0
SERVICE_STOPPED=0
INSTALL_STARTED=0
RELEASE_DATE=""
MAINTENANCE_OWNED=0
MAINTENANCE_FILE=""

read_config() {
  [[ -r "$ENV_FILE" ]] || return 0
  while IFS='=' read -r key value; do
    case "$key" in
      DEVBOX_UPDATE_REPOSITORY) SOURCE_REPOSITORY="$value" ;;
      DEVBOX_UPDATE_REF) SOURCE_REF="$value" ;;
      DEVBOX_VERSION) CURRENT_VERSION="$value" ;;
      DEVBOX_UPDATE_MODE) UPDATE_MODE="$value" ;;
      DEVBOX_UPDATE_ALLOW_UNSIGNED) ALLOW_UNSIGNED="$value" ;;
      DEVBOX_UPDATE_RELEASE_BASE) RELEASE_BASE="$value" ;;
      DEVBOX_UPDATE_PUBLIC_KEY) PUBLIC_KEY="$value" ;;
      DEVBOX_DATABASE_PATH) DATABASE_PATH="$value" ;;
      DEVBOX_HTTP_ADDR) HTTP_ADDR="$value" ;;
      DEVBOX_MIGRATIONS_DIR) MIGRATIONS_DIR="$value" ;;
      DEVBOX_FRONTEND_DIR) FRONTEND_DIR="$value" ;;
      DEVBOX_NGINX_SITES_AVAILABLE) NGINX_AVAILABLE="$value" ;;
      DEVBOX_NGINX_SITES_ENABLED) NGINX_ENABLED="$value" ;;
      DEVBOX_MASTER_KEY) ROLLBACK_KEY="$value" ;;
      DEVBOX_UPDATE_LAST_RELEASE_AT) LAST_RELEASE_AT="$value" ;;
    esac
  done <"$ENV_FILE"
}

cleanup() {
  unset ROLLBACK_KEY
  if [[ "$MAINTENANCE_OWNED" == 1 && ! -f "$BACKUP_ROOT/pending" ]]; then rm -f -- "$MAINTENANCE_FILE"; fi
  [[ -z "$TMP_DIR" ]] || rm -rf -- "$TMP_DIR"
}

rollback_update() {
  [[ "$SNAPSHOT_CREATED" == 1 && "$INSTALL_STARTED" == 1 ]] || {
    if [[ "$SERVICE_STOPPED" == 1 ]]; then systemctl start devbox.service >>"$LOG_FILE" 2>&1 || return 1; fi
    return 0
  }
  write_progress "running" 99 "rollback" "Przywracanie poprzednich plików, konfiguracji i SQLite."
  systemctl stop devbox.service >>"$LOG_FILE" 2>&1 || return 1
  DEVBOX_UPDATE_ROLLBACK_KEY="$ROLLBACK_KEY" "$BACKUP_DIR/restore-devbox" update-restore --file "$BACKUP_DIR/state.enc" >>"$LOG_FILE" 2>&1 || return 1
  systemctl daemon-reload >>"$LOG_FILE" 2>&1 || return 1
  if command -v nginx >/dev/null 2>&1; then nginx -t >>"$LOG_FILE" 2>&1 && systemctl reload nginx >>"$LOG_FILE" 2>&1 || return 1; fi
  systemctl start devbox.service >>"$LOG_FILE" 2>&1 || return 1
  "$BACKUP_DIR/restore-devbox" update-health --address "$HTTP_ADDR" --version "$CURRENT_VERSION" --timeout 45 >>"$LOG_FILE" 2>&1 || return 1
  rm -f -- "$BACKUP_ROOT/pending"
  log "Przywrócono i zweryfikowano poprzednią wersję $CURRENT_VERSION. Kopia: $BACKUP_DIR/state.enc"
}

on_error() {
  local rc="$1" message="Aktualizacja nie powiodła się na etapie: $PROGRESS_STAGE."
  trap - ERR INT TERM
  log "$message"
  if rollback_update; then
    if [[ "$SNAPSHOT_CREATED" == 1 && "$INSTALL_STARTED" == 1 ]]; then
      message+=" Poprzednia wersja została przywrócona i zweryfikowana."
    fi
  else
    message+=" ROLLBACK NIE POWIÓDŁ SIĘ. Zachowano kopię: $BACKUP_DIR/state.enc."
  fi
  write_progress "failed" "$PROGRESS_PERCENT" "$PROGRESS_STAGE" "$message" "Sprawdź $LOG_FILE."
  exit "$rc"
}
fail_update() { log "$1"; on_error 1; }

recover_pending() {
  [[ -f "$BACKUP_ROOT/pending" ]] || return 0
  local id
  IFS= read -r id <"$BACKUP_ROOT/pending"
  [[ "$id" =~ ^[0-9TZ-]+-[0-9]+$ ]] || fail_update "Niepoprawny identyfikator przerwanej aktualizacji."
  BACKUP_DIR="$BACKUP_ROOT/$id"
  [[ -x "$BACKUP_DIR/restore-devbox" && -f "$BACKUP_DIR/state.enc" && ! -L "$BACKUP_DIR" ]] || fail_update "Brak pełnej kopii przerwanej aktualizacji."
  CURRENT_VERSION="$(cat "$BACKUP_DIR/version")"
  HTTP_ADDR="$(cat "$BACKUP_DIR/address")"
  SNAPSHOT_CREATED=1; INSTALL_STARTED=1
  rollback_update || fail_update "Odzyskiwanie przerwanej aktualizacji nie powiodło się."
  SNAPSHOT_CREATED=0; INSTALL_STARTED=0; SERVICE_STOPPED=0
  write_progress "failed" 100 "rollback" "Odzyskano poprzednią wersję po przerwanej aktualizacji."
}

main() {
  [[ "$EUID" == 0 ]] || { printf 'Updater requires root.\n' >&2; return 1; }
  umask 077
  read_config
  CURRENT_VERSION="${CURRENT_VERSION:-unknown}"
  if [[ -z "$ROLLBACK_KEY" && -r "$(dirname "$DATABASE_PATH")/master.key" ]]; then ROLLBACK_KEY="$(cat "$(dirname "$DATABASE_PATH")/master.key")"; fi
  mkdir -p "$(dirname "$LOCK_FILE")" "$(dirname "$LOG_FILE")"
  exec 9>"$LOCK_FILE"
  if ! flock -n 9; then log "Aktualizacja już trwa; pomijam równoległe uruchomienie."; return 0; fi
  trap cleanup EXIT
  trap 'on_error $?' ERR
  trap 'on_error 130' INT TERM
  STARTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  [[ "$BACKUP_ROOT" == /* && ! -L "$BACKUP_ROOT" ]] || fail_update "Niebezpieczny katalog kopii aktualizacji."
  mkdir -p "$BACKUP_ROOT"
  [[ "$(stat -c %u "$BACKUP_ROOT")" == 0 ]] || fail_update "Katalog kopii musi należeć do root."
  chmod 0700 "$BACKUP_ROOT"
  MAINTENANCE_FILE="$(dirname "$DATABASE_PATH")/update-maintenance"
  if [[ -f "$BACKUP_ROOT/pending" ]]; then
    recover_pending
    rm -f -- "$MAINTENANCE_FILE"
  fi
  if [[ "${1:-}" == "--recover" ]]; then rm -f -- "$MAINTENANCE_FILE"; return 0; fi
  [[ -z "${1:-}" ]] || fail_update "Obsługiwany parametr: --recover."
  write_progress "running" 2 "starting" "Przygotowanie bezpiecznej aktualizacji."
  command -v systemctl >/dev/null 2>&1 || fail_update "Aktualizacja wymaga systemd."
  [[ -x "$LIBEXEC_DIR/devbox" ]] || fail_update "Brak zainstalowanego narzędzia DevBox."
  [[ -n "$ROLLBACK_KEY" ]] || fail_update "Brak oryginalnego master key; kopia nie może zostać zaszyfrowana."
  TMP_DIR="$(mktemp -d)"
  write_progress "running" 15 "download" "Pobieranie źródeł aktualizacji."
  case "$UPDATE_MODE" in
    signed)
      [[ "$RELEASE_BASE" =~ ^https://[A-Za-z0-9.-]+(:[0-9]+)?/[^[:space:]?#@]+$ ]] || fail_update "Niepoprawny HTTPS URL wydania."
      [[ -f "$PUBLIC_KEY" && ! -L "$PUBLIC_KEY" ]] || fail_update "Skonfiguruj zaufany publiczny klucz aktualizacji: $PUBLIC_KEY."
      [[ "$(stat -c %u "$PUBLIC_KEY")" == 0 ]] || fail_update "Zaufany klucz aktualizacji musi należeć do root."
      local key_mode
      key_mode="$(stat -c %a "$PUBLIC_KEY")"
      (( (8#$key_mode & 8#022) == 0 )) || fail_update "Klucz aktualizacji nie może być zapisywalny przez grupę/innych."
      for asset in devbox-manifest.json devbox-manifest.sig devbox-source.tar.gz; do
        curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 180 --max-filesize 67108864 "$RELEASE_BASE/$asset" -o "$TMP_DIR/$asset" >>"$LOG_FILE" 2>&1
      done
      write_progress "running" 30 "validation" "Weryfikacja podpisu Ed25519, sumy SHA-256 i archiwum."
      local verified
      verified="$("$LIBEXEC_DIR/devbox" verify-update --manifest "$TMP_DIR/devbox-manifest.json" --signature "$TMP_DIR/devbox-manifest.sig" --public-key "$PUBLIC_KEY" --archive "$TMP_DIR/devbox-source.tar.gz" --destination "$TMP_DIR/source" --minimum-time "$LAST_RELEASE_AT" 2>>"$LOG_FILE")"
      TARGET_VERSION="${verified%%$'\n'*}"
      RELEASE_DATE="${verified#*$'\n'}"
      ;;
    git)
      [[ "$ALLOW_UNSIGNED" == true ]] || fail_update "Tryb Git wymaga jawnego DEVBOX_UPDATE_ALLOW_UNSIGNED=true."
      [[ "$SOURCE_REPOSITORY" =~ ^https://[A-Za-z0-9.-]+/[^[:space:]?#@]+$ && "$SOURCE_REF" =~ ^[A-Za-z0-9][A-Za-z0-9._/-]*$ ]] || fail_update "Niepoprawne repozytorium lub ref."
      log "UWAGA: jawnie wybrany deweloperski tryb Git BEZ weryfikacji podpisu."
      git clone --depth 1 --branch "$SOURCE_REF" -- "$SOURCE_REPOSITORY" "$TMP_DIR/source" >>"$LOG_FILE" 2>&1
      TARGET_VERSION="$(git -C "$TMP_DIR/source" rev-parse HEAD)"
      ;;
    *) fail_update "DEVBOX_UPDATE_MODE musi być signed albo git." ;;
  esac
  [[ "$TARGET_VERSION" =~ ^[0-9a-f]{40}$ ]] || fail_update "Niepoprawna wersja docelowa."
  if [[ "$CURRENT_VERSION" == "$TARGET_VERSION" ]]; then write_progress "no_update" 100 "completed" "Zainstalowana wersja jest aktualna."; return 0; fi
  [[ -f "$TMP_DIR/source/install.sh" && -f "$TMP_DIR/source/backend/go.mod" && -f "$TMP_DIR/source/frontend/package-lock.json" ]] || fail_update "Niepełne źródła aktualizacji."
  write_progress "running" 32 "snapshot" "Sprawdzanie zadań i przygotowanie szyfrowanej kopii przed migracjami."
  local marker_tmp
  marker_tmp="$(mktemp "${MAINTENANCE_FILE}.XXXXXX")"
  printf 'update\n' >"$marker_tmp"
  chmod 0644 "$marker_tmp"
  mv -fT -- "$marker_tmp" "$MAINTENANCE_FILE"
  MAINTENANCE_OWNED=1
  "$LIBEXEC_DIR/devbox" update-check-idle --database "$DATABASE_PATH" >>"$LOG_FILE" 2>&1
  systemctl stop devbox.service >>"$LOG_FILE" 2>&1
  SERVICE_STOPPED=1
  "$LIBEXEC_DIR/devbox" update-check-idle --database "$DATABASE_PATH" >>"$LOG_FILE" 2>&1
  BACKUP_DIR="$BACKUP_ROOT/$(date -u +%Y%m%dT%H%M%SZ)-$$"
  mkdir -m 0700 "$BACKUP_DIR"
  cp -- "$LIBEXEC_DIR/devbox" "$BACKUP_DIR/restore-devbox"
  chmod 0700 "$BACKUP_DIR/restore-devbox"
  printf '%s\n' "$CURRENT_VERSION" >"$BACKUP_DIR/version"
  printf '%s\n' "$HTTP_ADDR" >"$BACKUP_DIR/address"
  DEVBOX_UPDATE_ROLLBACK_KEY="$ROLLBACK_KEY" "$BACKUP_DIR/restore-devbox" update-snapshot --file "$BACKUP_DIR/state.enc" -- \
    "$LIBEXEC_DIR" "$BIN_LINK" "$MIGRATIONS_DIR" "$FRONTEND_DIR" "$ENV_FILE" \
    "$DATABASE_PATH" "$DATABASE_PATH-wal" "$DATABASE_PATH-shm" "$(dirname "$DATABASE_PATH")/master.key" \
    "$NGINX_AVAILABLE" "$NGINX_ENABLED" \
    "${DEVBOX_NGINX_INCLUDE_FILE:-/etc/nginx/conf.d/devbox.conf}" \
    "${DEVBOX_SERVICE_FILE:-/etc/systemd/system/devbox.service}" \
    "${DEVBOX_UPDATE_SERVICE_FILE:-/etc/systemd/system/devbox-update.service}" \
    "${DEVBOX_UPDATE_TIMER_FILE:-/etc/systemd/system/devbox-update.timer}" \
    "${DEVBOX_SUDOERS_FILE:-/etc/sudoers.d/devbox}" >>"$LOG_FILE" 2>&1
  SNAPSHOT_CREATED=1
  printf '%s\n' "${BACKUP_DIR##*/}" >"$BACKUP_ROOT/pending.tmp"
  mv -f "$BACKUP_ROOT/pending.tmp" "$BACKUP_ROOT/pending"
  INSTALL_STARTED=1
  write_progress "running" 34 "environment" "Instalacja zweryfikowanej wersji; kopia została zapisana."
  DEVBOX_SOURCE_REPOSITORY="$SOURCE_REPOSITORY" DEVBOX_SOURCE_REF="$SOURCE_REF" DEVBOX_USE_CURRENT_SOURCE=1 \
    DEVBOX_UPDATE_PROGRESS_FILE="$PROGRESS_FILE" DEVBOX_UPDATE_STARTED_AT="$STARTED_AT" \
    DEVBOX_UPDATE_CURRENT_VERSION="$CURRENT_VERSION" DEVBOX_UPDATE_TARGET_VERSION="$TARGET_VERSION" \
    bash "$TMP_DIR/source/install.sh" --update >>"$LOG_FILE" 2>&1
  write_progress "running" 99 "restart" "Sprawdzanie nowego API, wersji i SQLite po restarcie."
  systemctl restart devbox.service >>"$LOG_FILE" 2>&1
  local new_address
  new_address="$(sed -n 's/^DEVBOX_HTTP_ADDR=//p' "$ENV_FILE" | tail -n 1)"
  "$BACKUP_DIR/restore-devbox" update-health --address "${new_address:-$HTTP_ADDR}" --version "$TARGET_VERSION" --timeout 45 >>"$LOG_FILE" 2>&1
  if [[ -n "$RELEASE_DATE" ]]; then
    # Preserve ownership/mode; only public monotonic release metadata is changed.
    local env_tmp="$ENV_FILE.update.$$"
    cp -p -- "$ENV_FILE" "$env_tmp"
    sed -i '/^DEVBOX_UPDATE_LAST_RELEASE_AT=/d' "$env_tmp"
    printf 'DEVBOX_UPDATE_LAST_RELEASE_AT=%s\n' "$RELEASE_DATE" >>"$env_tmp"
    chown --reference="$ENV_FILE" "$env_tmp"
    chmod --reference="$ENV_FILE" "$env_tmp"
    mv -f -- "$env_tmp" "$ENV_FILE"
  fi
  rm -f -- "$BACKUP_ROOT/pending"
  INSTALL_STARTED=0; SERVICE_STOPPED=0
  log "Zainstalowano i zweryfikowano $TARGET_VERSION. Kopia: $BACKUP_DIR/state.enc"
  CURRENT_VERSION="$TARGET_VERSION"
  write_progress "succeeded" 100 "completed" "Nowa wersja i baza SQLite działają poprawnie."
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then main "$@"; fi
