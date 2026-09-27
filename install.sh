#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_SOURCE="${BASH_SOURCE[0]:-}"
if [[ -n "$SCRIPT_SOURCE" && -f "$SCRIPT_SOURCE" ]]; then
  ROOT_DIR="$(cd "$(dirname "$SCRIPT_SOURCE")" && pwd)"
else
  ROOT_DIR="$PWD"
fi
SOURCE_REPOSITORY="${DEVBOX_SOURCE_REPOSITORY:-https://github.com/chmajster/DevBox-Uniwersal.git}"
SOURCE_REF="${DEVBOX_SOURCE_REF:-main}"
SOURCE_TMP_DIR=""
INSTALL_ROOT="${DEVBOX_INSTALL_ROOT:-/opt/devbox}"
LIBEXEC_DIR="${DEVBOX_LIBEXEC_DIR:-/usr/local/lib/devbox}"
BIN_LINK="${DEVBOX_BIN_LINK:-/usr/local/bin/devbox}"
CONFIG_DIR="${DEVBOX_CONFIG_DIR:-/etc/devbox}"
ENV_FILE="${DEVBOX_ENV_FILE:-$CONFIG_DIR/devbox.env}"
DATA_DIR="${DEVBOX_DATA_DIR:-/var/lib/devbox}"
SERVICE_FILE="${DEVBOX_SERVICE_FILE:-/etc/systemd/system/devbox.service}"
SUDOERS_FILE="${DEVBOX_SUDOERS_FILE:-/etc/sudoers.d/devbox}"
NGINX_INCLUDE_FILE="${DEVBOX_NGINX_INCLUDE_FILE:-/etc/nginx/conf.d/devbox.conf}"
NGINX_STATE_DIR="${DEVBOX_NGINX_STATE_DIR:-$DATA_DIR/nginx}"
LOG_FILE="${DEVBOX_INSTALL_LOG:-/var/log/devbox-installer.log}"
MODE=""
PURGE=0
COLOR=0
BOOTSTRAP_PASSWORD=""
BOOTSTRAP_USERNAME="admin"

if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
  COLOR=1
fi

color_for() {
  case "$1" in
    " OK ") printf '\033[32m' ;;
    INFO) printf '\033[36m' ;;
    WARN) printf '\033[33m' ;;
    FAIL) printf '\033[31m' ;;
    *) printf '' ;;
  esac
}

init_log() {
  local dir
  dir="$(dirname "$LOG_FILE")"
  if mkdir -p "$dir" 2>/dev/null && touch "$LOG_FILE" 2>/dev/null; then
    return 0
  fi
  LOG_FILE="${TMPDIR:-/tmp}/devbox-installer-${UID}.log"
  touch "$LOG_FILE"
}

emit() {
  local level="$1"
  shift
  local message="$*"
  printf '[%s] %s\n' "$level" "$message" >>"$LOG_FILE"
  if (( COLOR == 1 )); then
    printf '%b[%s]%b %s\n' "$(color_for "$level")" "$level" '\033[0m' "$message"
  else
    printf '[%s] %s\n' "$level" "$message"
  fi
}

stage() {
  emit INFO "[$1/8] $2"
}

fail() {
  emit FAIL "$*"
  exit 1
}

usage() {
  cat <<'USAGE'
DevBox Universal installer

Usage:
  ./install.sh --install
  curl -fsSL https://raw.githubusercontent.com/chmajster/DevBox-Uniwersal/main/install.sh | sudo bash -s -- --install
  ./install.sh --status
  ./install.sh --repair
  ./install.sh --update
  ./install.sh --uninstall [--purge]
  ./install.sh --help

Options:
  --purge   With --uninstall, also remove /var/lib/devbox and the devbox user.
            Data is preserved by default.
USAGE
}

parse_args() {
  MODE=""
  PURGE=0
  while (($#)); do
    case "$1" in
      --install|--status|--repair|--update|--uninstall|--help)
        [[ -z "$MODE" ]] || return 2
        MODE="$1"
        ;;
      --purge)
        PURGE=1
        ;;
      *)
        return 2
        ;;
    esac
    shift
  done
  [[ -n "$MODE" ]] || MODE="--help"
  if (( PURGE == 1 )) && [[ "$MODE" != "--uninstall" ]]; then
    return 2
  fi
}

require_root() {
  (( EUID == 0 )) || fail "Ta operacja wymaga uprawnień root. Uruchom przez sudo."
}

os_release_value() {
  local key="$1"
  [[ -r /etc/os-release ]] || return 1
  # shellcheck disable=SC1091
  . /etc/os-release
  case "$key" in
    ID) printf '%s' "${ID:-}" ;;
    VERSION_ID) printf '%s' "${VERSION_ID:-}" ;;
    PRETTY_NAME) printf '%s' "${PRETTY_NAME:-}" ;;
  esac
}

is_wsl() {
  [[ -n "${WSL_DISTRO_NAME:-}" ]] || grep -Eqi '(microsoft|wsl)' /proc/sys/kernel/osrelease 2>/dev/null
}

wsl_version() {
  if ! is_wsl; then
    printf '0'
  elif [[ -n "${WSL_INTEROP:-}" ]] || grep -Eqi '(wsl2|microsoft-standard)' /proc/sys/kernel/osrelease 2>/dev/null; then
    printf '2'
  else
    printf '1'
  fi
}

systemd_available() {
  [[ "$(ps -p 1 -o comm= 2>/dev/null | tr -d ' ')" == "systemd" ]] && command -v systemctl >/dev/null 2>&1
}

ensure_supported_linux() {
  local distro
  distro="$(os_release_value ID || true)"
  case "$distro" in
    ubuntu|debian) ;;
    *) fail "Nieobsługiwana dystrybucja: ${distro:-unknown}. Installer obsługuje Ubuntu i Debian." ;;
  esac
}

component_status() {
  local label="$1"
  shift
  local candidate
  for candidate in "$@"; do
    if command -v "$candidate" >/dev/null 2>&1; then
      emit " OK " "$(printf '%-9s' "$label") $(command -v "$candidate")"
      return 0
    fi
  done
  emit WARN "$(printf '%-9s' "$label") brak"
  return 1
}

show_components() {
  component_status Git git || true
  component_status Docker docker || true
  component_status Nginx nginx || true
  component_status MySQL mysql mariadb || true
  component_status PHP php || true
  component_status Composer composer || true
  component_status Python python3 python || true
  component_status pip pip3 pip || true
  component_status Go go || true
  component_status Node node || true
  component_status npm npm || true
}

upsert_env_file() {
  local file="$1" key="$2" value="$3"
  local tmp
  mkdir -p "$(dirname "$file")"
  tmp="$(mktemp)"
  if [[ -f "$file" ]]; then
    awk -v k="$key" -v v="$value" '
      BEGIN { found=0 }
      index($0, k "=") == 1 { if (!found) print k "=" v; found=1; next }
      { print }
      END { if (!found) print k "=" v }
    ' "$file" >"$tmp"
  else
    printf '%s=%s\n' "$key" "$value" >"$tmp"
  fi
  install -m 0640 "$tmp" "$file"
  rm -f "$tmp"
}

remove_env_key() {
  local file="$1" key="$2"
  [[ -f "$file" ]] || return 0
  local tmp
  tmp="$(mktemp)"
  awk -v k="$key" 'index($0, k "=") != 1 { print }' "$file" >"$tmp"
  install -m 0640 "$tmp" "$file"
  rm -f "$tmp"
}

select_mysql_package() {
  if apt-cache show default-mysql-server >/dev/null 2>&1; then
    printf 'default-mysql-server'
  else
    printf 'mysql-server'
  fi
}

install_packages() {
  local mysql_pkg
  mysql_pkg="$(select_mysql_package)"
  local packages=(ca-certificates curl sudo build-essential git docker.io nginx "$mysql_pkg" php-cli composer python3 python3-pip golang-go nodejs npm)
  local missing=()
  local pkg
  for pkg in "${packages[@]}"; do
    if ! dpkg-query -W -f='${Status}' "$pkg" 2>/dev/null | grep -q 'install ok installed'; then
      missing+=("$pkg")
    fi
  done
  if ((${#missing[@]} == 0)); then
    emit " OK " "Pakiety systemowe są już zainstalowane."
    return 0
  fi
  emit INFO "Instaluję brakujące pakiety z kontrolowanej listy: ${missing[*]}"
  DEBIAN_FRONTEND=noninteractive apt-get update -y >>"$LOG_FILE" 2>&1
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${missing[@]}" >>"$LOG_FILE" 2>&1
  emit " OK " "Pakiety systemowe zainstalowane."
}

cleanup_source_tree() {
  if [[ -n "$SOURCE_TMP_DIR" && -d "$SOURCE_TMP_DIR" ]]; then
    rm -rf "$SOURCE_TMP_DIR"
  fi
}

ensure_source_tree() {
  if [[ -f "$ROOT_DIR/backend/go.mod" && -f "$ROOT_DIR/frontend/package-lock.json" ]]; then
    return 0
  fi

  command -v git >/dev/null 2>&1 || fail "Git jest wymagany do pobrania źródeł DevBox."
  SOURCE_TMP_DIR="$(mktemp -d)"
  emit INFO "Pobieram źródła DevBox Universal (${SOURCE_REF})..."
  if ! git clone --depth 1 --branch "$SOURCE_REF" "$SOURCE_REPOSITORY" "$SOURCE_TMP_DIR/source" >>"$LOG_FILE" 2>&1; then
    fail "Nie udało się pobrać źródeł z $SOURCE_REPOSITORY (ref: $SOURCE_REF)."
  fi
  ROOT_DIR="$SOURCE_TMP_DIR/source"

  [[ -f "$ROOT_DIR/backend/go.mod" ]] || fail "Pobrane źródła nie zawierają backend/go.mod."
  [[ -f "$ROOT_DIR/frontend/package-lock.json" ]] || fail "Pobrane źródła nie zawierają frontend/package-lock.json."
  emit " OK " "Źródła DevBox Universal pobrane."
}

ensure_user_and_dirs() {
  if ! id devbox >/dev/null 2>&1; then
    useradd --system --home-dir "$DATA_DIR" --create-home --shell /usr/sbin/nologin devbox
  fi
  install -d -m 0755 "$INSTALL_ROOT" "$LIBEXEC_DIR"
  install -d -m 0750 -o devbox -g devbox "$DATA_DIR"
  install -d -m 0750 -o devbox -g devbox "$NGINX_STATE_DIR" "$NGINX_STATE_DIR/sites-available" "$NGINX_STATE_DIR/sites-enabled"
  install -d -m 0750 -o root -g devbox "$CONFIG_DIR"
  if getent group docker >/dev/null 2>&1; then
    usermod -aG docker devbox || true
  fi
}

build_backend() {
  [[ -f "$ROOT_DIR/backend/go.mod" ]] || fail "Brak backend/go.mod. Uruchom installer z katalogu repozytorium."
  command -v go >/dev/null 2>&1 || fail "Brak Go po instalacji pakietów."
  mkdir -p "$ROOT_DIR/.build"
  (cd "$ROOT_DIR/backend" && go build -trimpath -o "$ROOT_DIR/.build/devbox" ./cmd/devbox)
  (cd "$ROOT_DIR/backend" && go build -trimpath -o "$ROOT_DIR/.build/devbox-helper" ./cmd/devbox-helper)
  emit " OK " "Backend i privileged helper zbudowane."
}

build_frontend() {
  [[ -f "$ROOT_DIR/frontend/package-lock.json" ]] || fail "Brak frontend/package-lock.json."
  command -v npm >/dev/null 2>&1 || fail "Brak npm po instalacji pakietów."
  (cd "$ROOT_DIR/frontend" && npm ci >>"$LOG_FILE" 2>&1 && npm run build >>"$LOG_FILE" 2>&1)
  [[ -f "$ROOT_DIR/frontend/dist/index.html" ]] || fail "Build frontendu nie utworzył dist/index.html."
  emit " OK " "Frontend zbudowany."
}

install_artifacts() {
  ensure_user_and_dirs
  install -m 0755 -o root -g root "$ROOT_DIR/.build/devbox" "$LIBEXEC_DIR/devbox"
  install -m 0755 -o root -g root "$ROOT_DIR/.build/devbox-helper" "$LIBEXEC_DIR/devbox-helper"
  ln -sfn "$LIBEXEC_DIR/devbox" "$BIN_LINK"

  rm -rf "$INSTALL_ROOT/migrations" "$INSTALL_ROOT/frontend"
  install -d -m 0755 "$INSTALL_ROOT/migrations" "$INSTALL_ROOT/frontend/dist"
  cp -a "$ROOT_DIR/migrations/." "$INSTALL_ROOT/migrations/"
  cp -a "$ROOT_DIR/frontend/dist/." "$INSTALL_ROOT/frontend/dist/"

  upsert_env_file "$ENV_FILE" DEVBOX_HTTP_ADDR "127.0.0.1:8787"
  upsert_env_file "$ENV_FILE" DEVBOX_DATABASE_PATH "$DATA_DIR/devbox.db"
  upsert_env_file "$ENV_FILE" DEVBOX_MIGRATIONS_DIR "$INSTALL_ROOT/migrations"
  upsert_env_file "$ENV_FILE" DEVBOX_FRONTEND_DIR "$INSTALL_ROOT/frontend/dist"
  upsert_env_file "$ENV_FILE" DEVBOX_COOKIE_SECURE "false"
  upsert_env_file "$ENV_FILE" DEVBOX_VERSION "local"
  upsert_env_file "$ENV_FILE" DEVBOX_NGINX_SITES_AVAILABLE "$NGINX_STATE_DIR/sites-available"
  upsert_env_file "$ENV_FILE" DEVBOX_NGINX_SITES_ENABLED "$NGINX_STATE_DIR/sites-enabled"
  upsert_env_file "$ENV_FILE" DEVBOX_PRIVILEGED_HELPER "$LIBEXEC_DIR/devbox-helper"
  upsert_env_file "$ENV_FILE" DEVBOX_SUDO_BINARY "$(command -v sudo)"
  chown root:devbox "$ENV_FILE"
  chmod 0640 "$ENV_FILE"
  emit " OK " "Pliki aplikacji i konfiguracja zainstalowane."
}

install_nginx_integration() {
  local sudo_bin sudoers_tmp
  sudo_bin="$(command -v sudo || true)"
  [[ -n "$sudo_bin" ]] || fail "Brak sudo wymaganego do bezpiecznej obsługi Nginx."

  sudoers_tmp="$(mktemp)"
  cat >"$sudoers_tmp" <<EOF_SUDOERS
devbox ALL=(root) NOPASSWD: $LIBEXEC_DIR/devbox-helper validate-nginx
devbox ALL=(root) NOPASSWD: $LIBEXEC_DIR/devbox-helper reload-nginx
EOF_SUDOERS
  chmod 0440 "$sudoers_tmp"
  if ! visudo -cf "$sudoers_tmp" >>"$LOG_FILE" 2>&1; then
    rm -f "$sudoers_tmp"
    fail "Nieprawidłowa konfiguracja sudoers dla devbox-helper."
  fi
  install -m 0440 -o root -g root "$sudoers_tmp" "$SUDOERS_FILE"
  rm -f "$sudoers_tmp"

  install -d -m 0755 "$(dirname "$NGINX_INCLUDE_FILE")"
  cat >"$NGINX_INCLUDE_FILE" <<EOF_NGINX
# Managed by DevBox Universal.
# Dynamic site files are writable only inside DevBox state and loaded by root Nginx.
include $NGINX_STATE_DIR/sites-enabled/*.conf;
EOF_NGINX
  chmod 0644 "$NGINX_INCLUDE_FILE"

  if ! nginx -t >>"$LOG_FILE" 2>&1; then
    fail "Konfiguracja Nginx jest nieprawidłowa po dodaniu integracji DevBox. Szczegóły: $LOG_FILE"
  fi
  if systemd_available; then
    systemctl enable --now nginx.service >>"$LOG_FILE" 2>&1
    systemctl reload nginx.service >>"$LOG_FILE" 2>&1
  fi
  emit " OK " "Nginx używa kontrolowanego include z $NGINX_STATE_DIR; walidacja/reload działa przez devbox-helper."
}

generate_bootstrap_credentials() {
  if [[ -e "$DATA_DIR/devbox.db" ]]; then
    return 0
  fi
  if grep -q '^DEVBOX_BOOTSTRAP_ADMIN_USERNAME=' "$ENV_FILE" 2>/dev/null && grep -q '^DEVBOX_BOOTSTRAP_ADMIN_PASSWORD=' "$ENV_FILE" 2>/dev/null; then
    return 0
  fi
  BOOTSTRAP_PASSWORD="$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')"
  upsert_env_file "$ENV_FILE" DEVBOX_BOOTSTRAP_ADMIN_USERNAME "$BOOTSTRAP_USERNAME"
  upsert_env_file "$ENV_FILE" DEVBOX_BOOTSTRAP_ADMIN_PASSWORD "$BOOTSTRAP_PASSWORD"
  chown root:devbox "$ENV_FILE"
  chmod 0640 "$ENV_FILE"
}

write_service_unit() {
  cat >"$SERVICE_FILE" <<EOF_UNIT
[Unit]
Description=DevBox Universal control plane
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=devbox
Group=devbox
WorkingDirectory=$INSTALL_ROOT
EnvironmentFile=$ENV_FILE
ExecStart=$LIBEXEC_DIR/devbox serve
Restart=on-failure
RestartSec=3
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=$DATA_DIR

[Install]
WantedBy=multi-user.target
EOF_UNIT
  chmod 0644 "$SERVICE_FILE"
}

install_service() {
  write_service_unit
  if ! systemd_available; then
    emit WARN "systemd nie jest aktywny. W WSL włącz systemd w /etc/wsl.conf i uruchom ponownie dystrybucję."
    return 0
  fi
  systemctl daemon-reload
  systemctl enable --now devbox.service >>"$LOG_FILE" 2>&1
  emit " OK " "Usługa devbox.service jest aktywna."
}

wait_for_health() {
  if ! systemd_available; then
    emit WARN "Pominięto healthcheck usługi, ponieważ systemd nie działa."
    return 0
  fi
  for _ in {1..30}; do
    if curl -fsS --max-time 2 http://127.0.0.1:8787/api/v1/health >/dev/null 2>&1; then
      emit " OK " "Healthcheck API zakończony powodzeniem."
      if grep -q '^DEVBOX_BOOTSTRAP_ADMIN_USERNAME=' "$ENV_FILE" 2>/dev/null || grep -q '^DEVBOX_BOOTSTRAP_ADMIN_PASSWORD=' "$ENV_FILE" 2>/dev/null; then
        remove_env_key "$ENV_FILE" DEVBOX_BOOTSTRAP_ADMIN_USERNAME
        remove_env_key "$ENV_FILE" DEVBOX_BOOTSTRAP_ADMIN_PASSWORD
        chown root:devbox "$ENV_FILE"
        chmod 0640 "$ENV_FILE"
      fi
      return 0
    fi
    sleep 1
  done
  systemctl status devbox.service --no-pager >>"$LOG_FILE" 2>&1 || true
  fail "DevBox nie przeszedł healthchecku. Sprawdź $LOG_FILE i journalctl -u devbox."
}

run_doctor() {
  if [[ -x "$LIBEXEC_DIR/devbox" ]]; then
    set +e
    "$LIBEXEC_DIR/devbox" doctor
    local rc
    rc=$?
    set -e
    if (( rc == 0 )); then
      emit " OK " "devbox doctor nie wykrył błędów krytycznych."
    else
      emit WARN "devbox doctor zgłosił problem; szczegóły powyżej."
    fi
  else
    emit WARN "CLI devbox nie jest jeszcze zainstalowane."
  fi
}

run_install() {
  require_root
  stage 1 "Detekcja systemu i WSL"
  ensure_supported_linux
  local pretty wsl
  pretty="$(os_release_value PRETTY_NAME || true)"
  wsl="$(wsl_version)"
  emit " OK " "System: ${pretty:-Linux}; WSL=${wsl}; systemd=$(systemd_available && printf yes || printf no)"

  stage 2 "Komponenty systemowe i źródła"
  install_packages
  show_components
  ensure_source_tree

  stage 3 "Build backendu i helpera"
  build_backend

  stage 4 "Build frontendu"
  build_frontend

  stage 5 "Instalacja plików i konfiguracji"
  install_artifacts
  install_nginx_integration
  generate_bootstrap_credentials

  stage 6 "Instalacja usługi"
  install_service

  stage 7 "Healthcheck i diagnostyka"
  wait_for_health
  run_doctor

  stage 8 "Podsumowanie"
  emit " OK " "DevBox Universal zainstalowany. GUI: http://localhost:8787/"
  emit INFO "Log instalatora: $LOG_FILE"
  if [[ -n "$BOOTSTRAP_PASSWORD" ]]; then
    # Deliberately not written through emit(): credentials must never enter the installer log.
    printf '[INFO] Pierwsze logowanie: %s\n' "$BOOTSTRAP_USERNAME"
    printf '[INFO] Hasło jednorazowo wygenerowane podczas instalacji: %s\n' "$BOOTSTRAP_PASSWORD"
  fi
}

run_status() {
  stage 1 "System"
  local pretty
  pretty="$(os_release_value PRETTY_NAME || true)"
  emit INFO "${pretty:-unknown}; WSL=$(wsl_version)"
  stage 2 "Komponenty"
  show_components
  stage 3 "systemd"
  if systemd_available; then emit " OK " "systemd aktywny"; else emit WARN "systemd nieaktywny"; fi
  stage 4 "Usługa DevBox"
  if systemd_available && systemctl is-active --quiet devbox.service; then emit " OK " "devbox.service active"; else emit WARN "devbox.service nieaktywna"; fi
  stage 5 "Pliki"
  if [[ -x "$LIBEXEC_DIR/devbox" ]]; then
    emit " OK " "$LIBEXEC_DIR/devbox"
  else
    emit WARN "Brak binarki DevBox"
  fi
  stage 6 "Dane"
  if [[ -d "$DATA_DIR" ]]; then
    emit " OK " "$DATA_DIR"
  else
    emit WARN "Brak katalogu danych"
  fi
  stage 7 "GUI/API"
  if command -v curl >/dev/null 2>&1 && curl -fsS --max-time 2 http://127.0.0.1:8787/api/v1/health >/dev/null 2>&1; then emit " OK " "http://localhost:8787/"; else emit WARN "API nie odpowiada"; fi
  stage 8 "Doctor"
  run_doctor
}

run_uninstall() {
  require_root
  stage 1 "Zatrzymanie usługi"
  if systemd_available; then systemctl disable --now devbox.service >>"$LOG_FILE" 2>&1 || true; fi
  emit " OK " "Usługa zatrzymana lub nie była aktywna."
  stage 2 "Usunięcie unit file"
  rm -f "$SERVICE_FILE"
  if systemd_available; then systemctl daemon-reload; fi
  emit " OK " "Unit file usunięty."
  stage 3 "Usunięcie binarek"
  rm -f "$BIN_LINK" "$LIBEXEC_DIR/devbox" "$LIBEXEC_DIR/devbox-helper"
  rmdir "$LIBEXEC_DIR" 2>/dev/null || true
  emit " OK " "Binarki usunięte."
  stage 4 "Usunięcie aplikacji"
  rm -rf "$INSTALL_ROOT"
  emit " OK " "$INSTALL_ROOT usunięty."
  stage 5 "Konfiguracja"
  rm -f "$SUDOERS_FILE" "$NGINX_INCLUDE_FILE"
  rm -rf "$CONFIG_DIR"
  if command -v nginx >/dev/null 2>&1 && nginx -t >>"$LOG_FILE" 2>&1 && systemd_available && systemctl is-active --quiet nginx.service; then
    systemctl reload nginx.service >>"$LOG_FILE" 2>&1 || true
  fi
  emit " OK " "Konfiguracja DevBox, sudoers i include Nginx usunięte."
  stage 6 "Dane"
  if (( PURGE == 1 )); then
    rm -rf "$DATA_DIR"
    emit " OK " "Dane usunięte (--purge)."
  else
    emit INFO "Dane zachowane w $DATA_DIR."
  fi
  stage 7 "Konto systemowe"
  if (( PURGE == 1 )); then
    userdel devbox 2>/dev/null || true
    emit " OK " "Konto devbox usunięte."
  else
    emit INFO "Konto devbox pozostawione dla zachowanych danych."
  fi
  stage 8 "Podsumowanie"
  emit " OK " "Odinstalowanie zakończone. Pakiety współdzielone nie zostały usunięte."
}

main() {
  init_log
  trap cleanup_source_tree EXIT
  if ! parse_args "$@"; then
    usage >&2
    exit 2
  fi
  case "$MODE" in
    --install|--repair|--update) run_install ;;
    --status) run_status ;;
    --uninstall) run_uninstall ;;
    --help) usage ;;
  esac
}

if [[ "${BASH_SOURCE[0]:-$0}" == "$0" ]]; then
  main "$@"
fi
