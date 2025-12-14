Review all .md files under the project. 

We follow the industry best practices of Go programming
and DevOps industry. Our goal is to create the industry greatest
example of the tools. Review how people around Docker, RunC, Kubernetes, AWS, GCP
think before moving on with this project. 

We believe documentation is key. We maintain various .md files
to keep the project well documentated. It's part of our job
to update and clean up the .md files to replicate our state of
project understanding. 

We strongly believe in tests and integration tests. Project must
be tested, we should be sure tests are enough to handle. All bugs
are resolved in Test-first-approach. First test is added to reproduce,
next the fix is added to fix the test. Make sure you reproduce the
right problem and your fix fixes that. Tests are higher priority than the code!

Project specifics:

- Keep `README.md` and `docs/*.md` consistent with the actual implementation state.
- Community vs PRO is documentation-level for now (no feature gating in code yet).
- Host interactions should be via `stevedore-install.sh` and `stevedore.sh` (Docker-first).
- Installer should prefer a systemd unit (`stevedore.service`) to keep the container running across reboots (fall back to a Docker restart policy if systemd is unavailable).
- Persist state in the SQLCipher-encrypted SQLite DB (`system/stevedore.db`); avoid plaintext secret files (installer generates `system/db.key`).
- Repositories use Compose as the entrypoint (`docker-compose.yaml` preferred; fall back to other common names).
- Community v1 (planned): public HTTPS repositories only; PRO (planned): private repositories via SSH deploy keys/tokens.
- The container service runs the daemon via `stevedore -d`.
- Prefer POSIX `sh` for host scripts; target Ubuntu and Raspberry Pi OS.
- Add integration tests that run Stevedore in Docker; keep them runnable in GitHub Actions.
