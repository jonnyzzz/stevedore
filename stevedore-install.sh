#!/usr/bin/env sh
set -eu

STEVEDORE_HOST_ROOT="${STEVEDORE_HOST_ROOT:-/opt/stevedore}"
STEVEDORE_WRAPPER_PATH="${STEVEDORE_WRAPPER_PATH:-/usr/local/bin/stevedore.sh}"
STEVEDORE_CLI_PATH="${STEVEDORE_CLI_PATH:-/usr/local/bin/stevedore}"
STEVEDORE_SERVICE_NAME="${STEVEDORE_SERVICE_NAME:-stevedore}"
STEVEDORE_CONTAINER_NAME="${STEVEDORE_CONTAINER_NAME:-stevedore}"
STEVEDORE_IMAGE="${STEVEDORE_IMAGE:-stevedore:latest}"
STEVEDORE_CONTAINER_ENV="${STEVEDORE_CONTAINER_ENV:-${STEVEDORE_HOST_ROOT}/system/container.env}"
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

is_ssh_url() {
  url="${1:-}"
  case "$url" in
    git@*) return 0 ;;
    ssh://*) return 0 ;;
    *@*:*) return 0 ;;  # SCP-style: user@host:/path
  esac
  return 1
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

detect_docker_access() {
  if docker info >/dev/null 2>&1; then
    DOCKER_USE_SUDO=0
    return 0
  fi

  if sudo_cmd docker info >/dev/null 2>&1; then
    DOCKER_USE_SUDO=1
    return 0
  fi

  if have_systemd; then
    sudo_cmd systemctl start docker >/dev/null 2>&1 || true
    if sudo_cmd docker info >/dev/null 2>&1; then
      DOCKER_USE_SUDO=1
      return 0
    fi
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

ensure_admin_key() {
  key_path="${STEVEDORE_HOST_ROOT}/system/admin.key"

  if sudo_cmd test -f "${key_path}"; then
    return 0
  fi

  log "Generating admin API key: ${key_path}"
  if ! command -v base64 >/dev/null 2>&1; then
    die "base64 is required to generate the admin key"
  fi

  key="$(dd if=/dev/urandom bs=32 count=1 2>/dev/null | base64 | tr -d '\n')"
  [ -n "${key}" ] || die "Failed to generate admin key"

  sudo_cmd sh -c "umask 077; printf '%s\n' '${key}' > '${key_path}'"
}

have_systemd() {
  command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]
}

write_container_env() {
  log "Writing container env: ${STEVEDORE_CONTAINER_ENV}"
  sudo_cmd mkdir -p "$(dirname "${STEVEDORE_CONTAINER_ENV}")"

  sudo_cmd sh -c "umask 077; cat > '${STEVEDORE_CONTAINER_ENV}'" <<EOF
STEVEDORE_ROOT=/opt/stevedore
STEVEDORE_DB_KEY_FILE=/opt/stevedore/system/db.key
STEVEDORE_ADMIN_KEY_FILE=/opt/stevedore/system/admin.key
STEVEDORE_CONTAINER_NAME=${STEVEDORE_CONTAINER_NAME}
STEVEDORE_SOURCE_REPO=${git_repo}
STEVEDORE_SOURCE_REF=${git_branch}
EOF
}

build_image() {
  log "Building image: ${STEVEDORE_IMAGE}"
  docker_cmd build -t "${STEVEDORE_IMAGE}" .
}

install_systemd_service() {
  service_path="/etc/systemd/system/${STEVEDORE_SERVICE_NAME}.service"
  docker_bin="$(command -v docker)"

  log "Installing systemd service: ${service_path}"
  sudo_cmd sh -c "cat > '${service_path}'" <<EOF
[Unit]
Description=Stevedore daemon (Docker)
After=docker.service
Requires=docker.service

[Service]
Type=simple
Restart=always
RestartSec=2
ExecStartPre=-${docker_bin} rm -f ${STEVEDORE_CONTAINER_NAME}
ExecStart=${docker_bin} run --name ${STEVEDORE_CONTAINER_NAME} --env-file ${STEVEDORE_CONTAINER_ENV} -p 42107:42107 -v /var/run/docker.sock:/var/run/docker.sock -v ${STEVEDORE_HOST_ROOT}:/opt/stevedore ${STEVEDORE_IMAGE}
ExecStop=-${docker_bin} stop ${STEVEDORE_CONTAINER_NAME}
TimeoutStopSec=30

[Install]
WantedBy=multi-user.target
EOF

  sudo_cmd systemctl daemon-reload
  sudo_cmd systemctl enable "${STEVEDORE_SERVICE_NAME}"
  sudo_cmd systemctl restart "${STEVEDORE_SERVICE_NAME}"
}

start_container_without_systemd() {
  log "systemd not detected; starting container via docker restart policy"
  docker_cmd rm -f "${STEVEDORE_CONTAINER_NAME}" >/dev/null 2>&1 || true
  docker_cmd run -d --name "${STEVEDORE_CONTAINER_NAME}" --restart unless-stopped \
    --env-file "${STEVEDORE_CONTAINER_ENV}" \
    -p 42107:42107 \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v "${STEVEDORE_HOST_ROOT}:/opt/stevedore" \
    "${STEVEDORE_IMAGE}" >/dev/null
}

wait_for_container() {
  name="$1"
  i=0
  while [ "$i" -lt 30 ]; do
    if docker_cmd ps --format '{{.Names}}' | grep -qx "${name}"; then
      return 0
    fi
    i=$((i + 1))
    sleep 1
  done
  return 1
}

bootstrap_self_deployment() {
  deployment="${STEVEDORE_SELF_DEPLOYMENT:-stevedore}"

  if [ "${STEVEDORE_BOOTSTRAP_SELF:-1}" != "1" ]; then
    return 0
  fi

  if [ -z "${git_repo}" ] || [ -z "${git_branch}" ] || [ "${git_branch}" = "HEAD" ]; then
    log "Skipping self deployment bootstrap (source repo/branch not detected)."
    return 0
  fi

  if sudo_cmd test -d "${STEVEDORE_HOST_ROOT}/deployments/${deployment}"; then
    log "Self deployment already exists: ${deployment}"
    return 0
  fi

  log "Bootstrapping self deployment: ${deployment}"
  STEVEDORE_CONTAINER="${STEVEDORE_CONTAINER_NAME}" ./stevedore.sh repo add "${deployment}" "${git_repo}" --branch "${git_branch}"
}

main() {
  # Change to the script's directory (assumes .git is there)
  script_dir="$(cd "$(dirname "$0")" && pwd)" || die "Failed to determine script directory"
  cd "$script_dir" || die "Failed to change to script directory: $script_dir"

  [ "$(uname -s)" = "Linux" ] || die "This installer supports Linux only."
  [ -f "Dockerfile" ] || die "Dockerfile not found in script directory: $script_dir"

  # Assert: must be running from a git checkout (v0-1 requirement)
  # Skip this check if both STEVEDORE_GIT_URL and STEVEDORE_GIT_BRANCH are provided
  if [ -z "${STEVEDORE_GIT_URL:-}" ] || [ -z "${STEVEDORE_GIT_BRANCH:-}" ]; then
    [ -d ".git" ] || die "Not a git repository. Stevedore must be installed from a git clone.

To install Stevedore:
  1. Fork or clone the repository:
     git clone https://github.com/jonnyzzz/stevedore.git
     (or your own fork)
  2. Run the installer from within the cloned directory:
     cd stevedore
     ./stevedore-install.sh"
  fi

  git_repo=""
  git_branch=""

  # Allow override via STEVEDORE_GIT_URL (must be SSH URL)
  if [ -n "${STEVEDORE_GIT_URL:-}" ]; then
    if ! is_ssh_url "${STEVEDORE_GIT_URL}"; then
      die "STEVEDORE_GIT_URL must be an SSH URL (user@host:/path, git@host:path, or ssh://...), got: ${STEVEDORE_GIT_URL}"
    fi
    git_repo="${STEVEDORE_GIT_URL}"
    log "Using STEVEDORE_GIT_URL: ${git_repo}"
  elif command -v git >/dev/null 2>&1 && git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    git_repo="$(git remote get-url origin 2>/dev/null || true)"
  fi

  # Detect or override branch
  if [ -n "${STEVEDORE_GIT_BRANCH:-}" ]; then
    git_branch="${STEVEDORE_GIT_BRANCH}"
    log "Using STEVEDORE_GIT_BRANCH: ${git_branch}"
  elif command -v git >/dev/null 2>&1 && git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    git_branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
  fi

  # Validate git info was detected
  if [ -z "${git_repo}" ]; then
    log "WARNING: Could not detect git remote URL. Self-deployment bootstrap may not work correctly."
  fi
  if [ -z "${git_branch}" ] || [ "${git_branch}" = "HEAD" ]; then
    log "WARNING: Could not detect git branch. Self-deployment bootstrap may not work correctly."
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

  log "Creating state directory: ${STEVEDORE_HOST_ROOT}"
  sudo_cmd mkdir -p "${STEVEDORE_HOST_ROOT}/system" "${STEVEDORE_HOST_ROOT}/deployments"
  ensure_db_key
  ensure_admin_key

  write_container_env
  build_image

  if have_systemd; then
    install_systemd_service
  else
    start_container_without_systemd
  fi

  if ! wait_for_container "${STEVEDORE_CONTAINER_NAME}"; then
    die "Stevedore container did not start (name: ${STEVEDORE_CONTAINER_NAME})"
  fi

  log "Installing stevedore.sh wrapper to ${STEVEDORE_WRAPPER_PATH}"
  if command -v install >/dev/null 2>&1; then
    sudo_cmd install -m 0755 "./stevedore.sh" "${STEVEDORE_WRAPPER_PATH}"
  else
    sudo_cmd cp "./stevedore.sh" "${STEVEDORE_WRAPPER_PATH}"
    sudo_cmd chmod 0755 "${STEVEDORE_WRAPPER_PATH}"
  fi

  if [ "${STEVEDORE_CLI_PATH}" != "${STEVEDORE_WRAPPER_PATH}" ]; then
    log "Installing stevedore wrapper to ${STEVEDORE_CLI_PATH}"
    if command -v install >/dev/null 2>&1; then
      sudo_cmd install -m 0755 "./stevedore.sh" "${STEVEDORE_CLI_PATH}"
    else
      sudo_cmd cp "./stevedore.sh" "${STEVEDORE_CLI_PATH}"
      sudo_cmd chmod 0755 "${STEVEDORE_CLI_PATH}"
    fi
  fi

  log "Running doctor"
  STEVEDORE_CONTAINER="${STEVEDORE_CONTAINER_NAME}" ./stevedore.sh doctor

  bootstrap_self_deployment

  log "Done."
  log "Next:"
  log "  stevedore doctor"
  log "  stevedore repo add <deployment> <git-url> --branch <branch>"
}

main "$@"
