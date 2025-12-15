import os
import shutil
import subprocess
import tempfile
import unittest
import uuid


def _run(cmd, *, cwd, env=None, check=True):
    completed = subprocess.run(
        cmd,
        cwd=cwd,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        check=False,
    )
    if check and completed.returncode != 0:
        raise AssertionError(
            f"Command failed ({completed.returncode}): {cmd}\n\n{completed.stdout}"
        )
    return completed.stdout


class UbuntuInstallSmokeTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if shutil.which("docker") is None:
            raise unittest.SkipTest("docker is not installed")

        repo_root = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
        cls._repo_root = repo_root

    def _cleanup_host(self, *, container_names, image_tags, state_roots):
        for name in container_names:
            _run(["docker", "rm", "-f", name], cwd=self._repo_root, check=False)
        for tag in image_tags:
            _run(["docker", "rmi", "-f", tag], cwd=self._repo_root, check=False)
        for root in state_roots:
            shutil.rmtree(root, ignore_errors=True)

    def test_installer_on_ubuntu_container(self):
        run_id = uuid.uuid4().hex[:8]

        container1 = f"stevedore-it-{run_id}-1"
        image1 = f"stevedore:it-{run_id}-1"
        state1 = os.path.join(self._repo_root, ".tmp", f"install-{run_id}-1")

        container2 = f"stevedore-it-{run_id}-2"
        image2 = f"stevedore:it-{run_id}-2"
        state2 = os.path.join(self._repo_root, ".tmp", f"install-{run_id}-2")

        os.makedirs(os.path.dirname(state1), exist_ok=True)

        try:
            script = f"""
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y git ca-certificates

install_once() {{
  local state_root="$1"
  local container_name="$2"
  local image_tag="$3"
  local how="$4"

  rm -rf "${{state_root}}"
  mkdir -p "${{state_root}}"

  export STEVEDORE_HOST_ROOT="${{state_root}}"
  export STEVEDORE_CONTAINER_NAME="${{container_name}}"
  export STEVEDORE_IMAGE="${{image_tag}}"
  export STEVEDORE_ALLOW_UPSTREAM_MAIN=1
  export STEVEDORE_ASSUME_YES=1
  export STEVEDORE_BOOTSTRAP_SELF=0

  if [ "${{how}}" = "exec" ]; then
    ./stevedore-install.sh
  elif [ "${{how}}" = "sh" ]; then
    sh stevedore-install.sh
  else
    echo "unknown install mode: ${{how}}" >&2
    exit 2
  fi

  export STEVEDORE_CONTAINER="${{container_name}}"

  /usr/local/bin/stevedore doctor
  /usr/local/bin/stevedore version

  /usr/local/bin/stevedore repo add demo git@github.com:acme/demo.git --branch main
  /usr/local/bin/stevedore repo list | grep -qx demo

  /usr/local/bin/stevedore param set demo DEMO_KEY demo-value
  test "$(/usr/local/bin/stevedore param get demo DEMO_KEY)" = "demo-value"

  docker rm -f "${{container_name}}" >/dev/null
}}

install_once "{state1}" "{container1}" "{image1}" exec
install_once "{state2}" "{container2}" "{image2}" sh
"""

            _run(
                [
                    "docker",
                    "run",
                    "--rm",
                    "-v",
                    "/var/run/docker.sock:/var/run/docker.sock",
                    "-v",
                    f"{self._repo_root}:{self._repo_root}",
                    "-w",
                    self._repo_root,
                    "ubuntu:22.04",
                    "bash",
                    "-lc",
                    script,
                ],
                cwd=self._repo_root,
            )
        finally:
            self._cleanup_host(
                container_names=[container1, container2],
                image_tags=[image1, image2],
                state_roots=[state1, state2],
            )

