#!/usr/bin/env sh
set -eu

STEVEDORE_CONTAINER="${STEVEDORE_CONTAINER:-stevedore}"
STEVEDORE_BIN="${STEVEDORE_BIN:-/app/stevedore}"
DOCKER_USE_SUDO=0

if ! command -v docker >/dev/null 2>&1; then
  printf '%s\n' "ERROR: docker is not installed (required)" >&2
  exit 1
fi

docker_cmd() {
  if [ "${DOCKER_USE_SUDO}" = "1" ]; then
    sudo docker "$@"
    return
  fi

  docker "$@"
}

detect_docker_access() {
  if docker info >/dev/null 2>&1; then
    DOCKER_USE_SUDO=0
    return 0
  fi

  if command -v sudo >/dev/null 2>&1 && sudo docker info >/dev/null 2>&1; then
    DOCKER_USE_SUDO=1
    return 0
  fi

  printf '%s\n' "ERROR: docker is installed but not accessible; add your user to the docker group or run with sudo." >&2
  exit 1
}

detect_docker_access

if ! docker_cmd ps --format '{{.Names}}' | grep -qx "${STEVEDORE_CONTAINER}"; then
  printf '%s\n' "ERROR: Stevedore container '${STEVEDORE_CONTAINER}' is not running." >&2
  printf '%s\n' "If installed with stevedore-install.sh, start it with: sudo systemctl start stevedore" >&2
  exit 1
fi

if [ -t 0 ] && [ -t 1 ]; then
  exec docker_cmd exec -it "${STEVEDORE_CONTAINER}" "${STEVEDORE_BIN}" "$@"
fi

exec docker_cmd exec -i "${STEVEDORE_CONTAINER}" "${STEVEDORE_BIN}" "$@"
