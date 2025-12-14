#!/usr/bin/env sh
set -eu

STEVEDORE_HOST_ROOT="${STEVEDORE_HOST_ROOT:-/opt/stevedore}"
STEVEDORE_ENV_FILE="${STEVEDORE_ENV_FILE:-.env}"
STEVEDORE_WRAPPER_PATH="${STEVEDORE_WRAPPER_PATH:-/usr/local/bin/stevedore.sh}"
DOCKER_USE_SUDO=0

log() {
  printf '%s\n' "$*" >&2
}

die() {
  log "ERROR: $*"
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Required command not found: $1"
}

sudo_cmd() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
    return
  fi

  command -v sudo >/dev/null 2>&1 || die "sudo is required (run as root or install sudo)"
  sudo "$@"
}

docker_cmd() {
  if [ "${DOCKER_USE_SUDO}" = "1" ]; then
    sudo_cmd docker "$@"
    return
  fi

  docker "$@"
}

is_upstream_repo_main() {
  repo="${1:-}"
  branch="${2:-}"

  [ "$branch" = "main" ] || return 1

  case "$repo" in
    git@github.com:jonnyzzz/stevedore.git) return 0 ;;
    https://github.com/jonnyzzz/stevedore) return 0 ;;
    https://github.com/jonnyzzz/stevedore.git) return 0 ;;
    ssh://git@github.com/jonnyzzz/stevedore.git) return 0 ;;
  esac

  return 1
}

confirm_or_exit() {
  prompt="$1"
  if [ "${STEVEDORE_ASSUME_YES:-}" = "1" ]; then
    return 0
  fi

  printf '%s [y/N] ' "$prompt" >&2
  read -r answer || true
  case "${answer:-}" in
    y|Y|yes|YES) return 0 ;;
  esac
  return 1
}

ensure_docker() {
  if command -v docker >/dev/null 2>&1; then
    return 0
  fi

  log "Docker not found; attempting to install it."

  if command -v curl >/dev/null 2>&1; then
    :
  elif command -v apt-get >/dev/null 2>&1; then
    sudo_cmd apt-get update
    sudo_cmd apt-get install -y curl ca-certificates
  else
    die "Neither docker nor a supported package manager was found. Install Docker manually."
  fi

  if command -v curl >/dev/null 2>&1; then
    sudo_cmd sh -c "curl -fsSL https://get.docker.com | sh"
  fi

  need_cmd docker
}

ensure_compose_plugin() {
  if docker_cmd compose version >/dev/null 2>&1; then
    return 0
  fi

  log "Docker Compose plugin not found; attempting to install it."
  if command -v apt-get >/dev/null 2>&1; then
    sudo_cmd apt-get update
    sudo_cmd apt-get install -y docker-compose-plugin
  fi

  docker_cmd compose version >/dev/null 2>&1 || die "docker compose is still unavailable; install the Compose plugin."
}

detect_docker_access() {
  if docker info >/dev/null 2>&1; then
    DOCKER_USE_SUDO=0
    return 0
  fi

  if sudo_cmd docker info >/dev/null 2>&1; then
    DOCKER_USE_SUDO=1
    return 0
  fi

  die "Docker is installed but not accessible. Add your user to the 'docker' group or run with sudo."
}

ensure_db_key() {
  key_path="${STEVEDORE_HOST_ROOT}/system/db.key"

  if sudo_cmd test -f "${key_path}"; then
    return 0
  fi

  log "Generating database encryption key: ${key_path}"
  if ! command -v base64 >/dev/null 2>&1; then
    die "base64 is required to generate the database key"
  fi

  key="$(dd if=/dev/urandom bs=32 count=1 2>/dev/null | base64 | tr -d '\n')"
  [ -n "${key}" ] || die "Failed to generate database key"

  sudo_cmd sh -c "umask 077; printf '%s\n' '${key}' > '${key_path}'"
}

main() {
  [ "$(uname -s)" = "Linux" ] || die "This installer supports Linux only."
  [ -f "docker-compose.yml" ] || die "Run this script from the repository root (docker-compose.yml not found)."

  git_repo=""
  git_branch=""
  if command -v git >/dev/null 2>&1 && git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    git_repo="$(git remote get-url origin 2>/dev/null || true)"
    git_branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
  fi

  if is_upstream_repo_main "$git_repo" "$git_branch"; then
    log "WARNING: Installing from upstream 'jonnyzzz/stevedore' main can cause unexpected redeploys."
    log "Recommendation: fork the repository and install from your fork."
    if [ "${STEVEDORE_ALLOW_UPSTREAM_MAIN:-}" != "1" ]; then
      confirm_or_exit "Continue anyway?" || die "Aborted. Fork the repo (recommended) or set STEVEDORE_ALLOW_UPSTREAM_MAIN=1."
    fi
  fi

  ensure_docker
  detect_docker_access
  ensure_compose_plugin

  log "Creating state directory: ${STEVEDORE_HOST_ROOT}"
  sudo_cmd mkdir -p "${STEVEDORE_HOST_ROOT}/system" "${STEVEDORE_HOST_ROOT}/deployments"
  ensure_db_key

  log "Writing ${STEVEDORE_ENV_FILE}"
  cat >"${STEVEDORE_ENV_FILE}" <<EOF
STEVEDORE_HOST_ROOT=${STEVEDORE_HOST_ROOT}
STEVEDORE_SOURCE_REPO=${git_repo}
STEVEDORE_SOURCE_REF=${git_branch}
EOF

  log "Starting Stevedore container"
  docker_cmd compose up -d --build

  log "Installing stevedore.sh wrapper to ${STEVEDORE_WRAPPER_PATH}"
  if command -v install >/dev/null 2>&1; then
    sudo_cmd install -m 0755 "./stevedore.sh" "${STEVEDORE_WRAPPER_PATH}"
  else
    sudo_cmd cp "./stevedore.sh" "${STEVEDORE_WRAPPER_PATH}"
    sudo_cmd chmod 0755 "${STEVEDORE_WRAPPER_PATH}"
  fi

  log "Done."
  log "Next:"
  log "  stevedore.sh doctor"
  log "  stevedore.sh repo add <deployment> <git-url> --branch <branch>"
}

main "$@"
