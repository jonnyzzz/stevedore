---
title: "Production Notes: Deploying Stevedore on a Raspberry Pi"
date: 2026-01-01
---

Today is the day. I'm putting Stevedore into production on my Raspberry Pi host (`rp16g`). This is
the real deal: no hacks, no shortcuts, just the supported install path and careful verification.

Stevedore is designed for this exact setup: a single Docker-first control plane, a systemd service
when available, and a clear separation between the host and the container runtime. The goal is to
leave the machine in a stable, boring state and make future updates predictable.

## Guardrails

Before touching production, I’m setting a few guardrails:

* Use the official installer (`./stevedore-install.sh`), not ad-hoc docker run commands.
* Keep state under `/opt/stevedore` (default layout).
* Prefer systemd (`stevedore.service`) to ensure the container survives reboots.
* Never paste secrets into this post. Keys stay on the host, paths are fine to mention.

## Deployment Log

I'll keep this log updated before and after every command I run. It’s part checklist, part
storytelling, and a reminder that production should be deliberate.

- Before: `ssh jonnyzzz@rp16g 'uname -a; cat /etc/os-release; docker --version; docker compose version; git --version; systemctl --version; systemctl is-system-running || true; which stevedore || true; docker ps --format "{{.Names}}" | grep -E "^stevedore$" || true'` — gather host facts and check for any existing Stevedore install.
- After: host is Debian 13 (trixie) on arm64, Docker 29.1.3 with Compose v5.0.0, Git 2.47.3, systemd 257 running; no existing `stevedore` binary or container detected.
- Before: `ssh jonnyzzz@rp16g 'sudo -n true'` — verify passwordless sudo is available for non-interactive installation.
- After: sudo is available without a password prompt.
- Before: `ssh jonnyzzz@rp16g 'set -e; if [ -d ~/stevedore/.git ]; then cd ~/stevedore && git pull --ff-only; else git clone https://github.com/jonnyzzz/stevedore.git ~/stevedore; fi; cd ~/stevedore; STEVEDORE_ASSUME_YES=1 STEVEDORE_ALLOW_UPSTREAM_MAIN=1 ./stevedore-install.sh'` — fetch the latest code and run the supported installer with non-interactive flags.
- After: install succeeded; systemd unit `stevedore.service` is installed and running, the `stevedore` wrapper is in `/usr/local/bin`, and the daemon reports version `0.7.44` at commit `a79895a...`. The installer also bootstrapped the self-deployment and printed a deploy key that must be added in GitHub before self-update can pull from the repo.
- Before: `ssh jonnyzzz@rp16g 'systemctl is-active stevedore.service; stevedore doctor'` — verify the service is active and the wrapper can reach the daemon.
- After: systemd reports `active`, and `stevedore doctor` shows the daemon running with the expected version and a single deployment (the self-managed `stevedore` repo).
- Before: `gh auth status` — check whether GitHub CLI is authenticated so I can add the deploy key programmatically.
- After: GitHub CLI is authenticated with repo access, so I can add the deploy key via the API.
- Before: `ssh jonnyzzz@rp16g 'stevedore repo key stevedore'` — retrieve the public deploy key for the self-managed repository.
- After: captured the deploy public key for the `stevedore` deployment (not pasted here).
- Before: `gh api -X POST repos/jonnyzzz/stevedore/keys -f title='stevedore-rp16g' -f key='<public-key>' -f read_only=true` — register the deploy key with GitHub (key omitted in this log).
- After: deploy key created via API, but the response reports `read_only: false` — I need to fix this to enforce read-only access.
- Before: `gh api -X DELETE repos/jonnyzzz/stevedore/keys/139388419` — remove the incorrectly configured deploy key.
- After: key removed; ready to re-add with explicit read-only permissions.
- Before: `gh api -X POST repos/jonnyzzz/stevedore/keys -f title='stevedore-rp16g' -f key='<public-key>' -F read_only=true` — re-add the deploy key with a boolean read-only flag.
- After: deploy key re-added; GitHub confirms `read_only: true`.
- Before: `ssh jonnyzzz@rp16g 'stevedore deploy sync stevedore'` — verify the self-deployment can authenticate and sync via the deploy key.
- After: self-deployment synced successfully to `main@a79895a`.
- Before: `git status -sb` — inspect local changes after the deployment diary updates.
- After: the new deployment blog post is the only untracked file.
- Before: `go build ./...` — ensure the local tree still compiles after the documentation updates.
- After: build completed successfully.
- Before: `go test ./...` — run the full test suite to keep the worktree verified.
- After: all tests passed (including integration).
- Before: `git add blogs/04-production-raspberry-pi-deployment.md` — stage the deployment story for commit.
- After: blog post staged.
- Before: `git add blogs/04-production-raspberry-pi-deployment.md` — stage the final version of the deployment story (including the command log).
- After: blog post staged with the complete log.
- Before: `git commit -m "blog: production deploy on rp16g"` — record the deployment story in the repo.
- After: blog post committed with the production deployment narrative and log.
- Before: `git push` — publish the deployment story alongside the latest code.
- After: blog post pushed to `origin/main`.
- Before: `git rev-parse HEAD` — record the local commit hash to compare against the Pi.
- After: local HEAD is `1f9c53c796f1df52e729a21a58dad1b8d4035bfd`.
- Before: `ssh jonnyzzz@rp16g 'stevedore version'` — capture the running binary version on the Pi.
- After: Pi reports `stevedore 0.7.44` at commit `a79895ab54d1` (still the previous build).
- Before: `ssh jonnyzzz@rp16g 'stevedore check stevedore'` — check whether the self-deployment sees the latest commit.
- After: check reports `Updates available` (current `a79895ab54d1`, remote `1f9c53c796f1`).
- Before: inspect the code paths that print deploy key instructions (repo add + installer) to add a GitHub CLI command.
- After: repo add instructions live in `main.go` (`runRepoTo`), and installer instructions are in `stevedore-install.sh`.
- Before: `go test ./...` — run the full Go test suite after adding the GitHub CLI deploy key guidance.
- After: `go test ./...` passed (unit + integration packages).
- Before: `go vet ./...` — confirm no new vet warnings after the CLI output changes.
- After: `go vet ./...` reported no issues.
- Before: `git add main.go` — stage the CLI output change for the GitHub deploy key command.
- After: `git add main.go` failed with `fatal: Unable to create '/Users/jonnyzzz/Work/stevedore/.git/index.lock': Operation not permitted`.
- Before: `ls -ld .git` — verify repository metadata permissions after the index.lock error.
- After: `.git` is owned by `jonnyzzz` with writable permissions (`drwxr-xr-x`).
- Before: `ls -lO .git` — check for immutable flags that could block index.lock creation.
- After: no immutable flags set in `.git`; need to retry staging with elevated permissions.
- Before: `git add main.go` (escalated) — retry staging now that permissions were verified.
- After: `git add main.go` succeeded with escalated permissions.
- Before: `git commit -m "cli: suggest gh deploy key command"` — record the CLI output change.
- After: commit `03ad99e` recorded the CLI deploy key guidance update.
- Before: `git add docs/REPOSITORIES.md blogs/03-getting-started-with-stevedore.md` — stage doc updates with the gh CLI guidance.
- After: docs staged for the GitHub CLI deploy key instructions.
- Before: `git commit -m "docs: add gh deploy key instructions"` — commit the documentation updates.
- After: commit `83cae76` captured the docs and blog tutorial updates.
- Before: `git add blogs/04-production-raspberry-pi-deployment.md` — stage the command log updates.
- After: `git add blogs/04-production-raspberry-pi-deployment.md` completed (staging will be refreshed after remaining log entries).
- Before: `git add blogs/04-production-raspberry-pi-deployment.md` (final) — refresh staging with the completed command log.
- After: `git add blogs/04-production-raspberry-pi-deployment.md` completed (final staging).
- Before: `git commit -m "blog: log gh deploy key guidance work"` — capture the updated command log.
- After: blog log commit recorded.
- Before: `git push` — publish the CLI guidance updates and the refreshed deployment log.
- After: `git push` completed.
