import base64
import os
import shutil
import subprocess
import tempfile
import unittest


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


def _run_result(cmd, *, cwd, env=None):
    return subprocess.run(
        cmd,
        cwd=cwd,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        check=False,
    )


def _db_header_bytes(image, mount, *, cwd):
    header_b64 = _run(
        [
            "docker",
            "run",
            "--rm",
            "-v",
            mount,
            image,
            "sh",
            "-c",
            "dd if=/opt/stevedore/system/stevedore.db bs=1 count=16 2>/dev/null | base64 | tr -d '\\n'",
        ],
        cwd=cwd,
    ).strip()
    try:
        return base64.b64decode(header_b64)
    except Exception as e:
        raise AssertionError(f"Unexpected base64 db header: {header_b64!r}") from e


class SmokeTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if shutil.which("docker") is None:
            raise unittest.SkipTest("docker is not installed")

        repo_root = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
        cls._repo_root = repo_root
        cls._image = "stevedore:ci"

        _run(["docker", "build", "-t", cls._image, "."], cwd=repo_root)

    def test_repo_and_parameters_roundtrip(self):
        with tempfile.TemporaryDirectory() as state_root:
            mount = f"{state_root}:/opt/stevedore"
            system_dir = os.path.join(state_root, "system")

            with open(
                os.path.join(self._repo_root, "VERSION"), "r", encoding="utf-8"
            ) as f:
                expected_version = f.read().strip()
            version_out = _run(
                ["docker", "run", "--rm", self._image, "./stevedore", "version"],
                cwd=self._repo_root,
            ).strip()
            self.assertTrue(version_out.startswith(f"stevedore {expected_version}"))
            self.assertNotIn("unknown", version_out)
            self.assertNotIn("://", version_out)

            missing_key = _run_result(
                ["docker", "run", "--rm", "-v", mount, self._image, "./stevedore", "doctor"],
                cwd=self._repo_root,
            )
            self.assertNotEqual(missing_key.returncode, 0)
            self.assertIn("database key is missing", missing_key.stdout)

            os.makedirs(system_dir, exist_ok=True)
            with open(os.path.join(system_dir, "db.key"), "w", encoding="utf-8") as f:
                f.write("test-db-key\n")

            _run(
                ["docker", "run", "--rm", "-v", mount, self._image, "./stevedore", "doctor"],
                cwd=self._repo_root,
            )

            out = _run(
                [
                    "docker",
                    "run",
                    "--rm",
                    "-v",
                    mount,
                    self._image,
                    "./stevedore",
                    "repo",
                    "add",
                    "demo",
                    "git@github.com:acme/demo.git",
                    "--branch",
                    "main",
                ],
                cwd=self._repo_root,
            )
            self.assertIn("ssh-ed25519", out)

            pub = _run(
                [
                    "docker",
                    "run",
                    "--rm",
                    "-v",
                    mount,
                    self._image,
                    "./stevedore",
                    "repo",
                    "key",
                    "demo",
                ],
                cwd=self._repo_root,
            ).strip()
            self.assertTrue(pub.startswith("ssh-ed25519 "))

            _run(
                [
                    "docker",
                    "run",
                    "--rm",
                    "-v",
                    mount,
                    self._image,
                    "./stevedore",
                    "param",
                    "set",
                    "demo",
                    "DATABASE_URL",
                    "postgres://user:pass@db:5432/app",
                ],
                cwd=self._repo_root,
            )

            value = _run(
                [
                    "docker",
                    "run",
                    "--rm",
                    "-v",
                    mount,
                    self._image,
                    "./stevedore",
                    "param",
                    "get",
                    "demo",
                    "DATABASE_URL",
                ],
                cwd=self._repo_root,
            )
            self.assertEqual(value, "postgres://user:pass@db:5432/app")

            db_path = os.path.join(state_root, "system", "stevedore.db")
            self.assertTrue(os.path.exists(db_path))

            header = _db_header_bytes(self._image, mount, cwd=self._repo_root)
            self.assertNotEqual(header, b"SQLite format 3\x00")

            wrong_key = _run_result(
                [
                    "docker",
                    "run",
                    "--rm",
                    "-v",
                    mount,
                    "-e",
                    "STEVEDORE_DB_KEY=wrong",
                    self._image,
                    "./stevedore",
                    "param",
                    "get",
                    "demo",
                    "DATABASE_URL",
                ],
                cwd=self._repo_root,
            )
            self.assertNotEqual(wrong_key.returncode, 0)

            legacy_path = os.path.join(
                state_root, "deployments", "demo", "parameters", "DATABASE_URL.txt"
            )
            self.assertFalse(os.path.exists(legacy_path))
