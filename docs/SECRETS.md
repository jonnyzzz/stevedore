# Secrets and Parameters

Stevedore needs a way to pass configuration (including secrets) into deployments without committing
them to Git.

## Default (Community): SQLite Parameters Store

Parameters are stored in a single SQLite database file:

`/opt/stevedore/system/stevedore.db`

The CLI (`stevedore.sh param …`) is responsible for writing/reading these values.

### Encryption at rest (SQLCipher)

- The SQLite database is encrypted on disk using SQLCipher.
- The database password is generated during installation and stored at `/opt/stevedore/system/db.key` (back it up).
- The container reads the password from:
  - `STEVEDORE_DB_KEY` (direct value), or
  - `STEVEDORE_DB_KEY_FILE` (file path), defaulting to `/opt/stevedore/system/db.key`.

This is still not a full secrets-management story (key rotation, audit, external backends), but it
keeps secrets out of Git and encrypts them at rest.

## Backup and Recovery

- Losing `db.key` means losing access to all stored parameters (the database cannot be decrypted).
- Back up both `/opt/stevedore/system/db.key` and `/opt/stevedore/system/stevedore.db` together.

## Secret Redaction in Logs (planned)

Stevedore will stream workload container logs into files under the state directory. To reduce
accidental leaks, Stevedore should apply best-effort redaction before writing logs:

- Never print secret values explicitly in Stevedore logs.
- When writing workload logs, replace known secret values (from the parameters DB) with `***`.

This is not a perfect guarantee (secrets can be transformed/encoded by applications), so the goal is
to reduce obvious leaks and document the limits clearly.

## SSH Keys (PRO, planned v4)

SSH deploy keys (for private Git repositories) are secrets.

Planned approach:

- Store SSH private keys encrypted in the SQLCipher database (`/opt/stevedore/system/stevedore.db`).
- Do not write key material to disk (no `id_ed25519` files under the state directory).
- Provide an SSH agent from the daemon and forward/mount it into the git worker container only when needed.

## How Parameters Reach Containers (Options)

We need to decide which approach becomes the default:

1. **Environment variables (simple)**
   - Stevedore renders a `.env` file (or equivalent) for `docker compose`.
   - Pros: simple and Compose-native.
   - Cons: env vars can leak via process/env inspection.
2. **File mounts**
   - Stevedore writes files under the deployment directory and mounts them read-only.
   - Pros: avoids env var leakage.
   - Cons: apps must read files.
3. **Compose “secrets:”**
   - Local-file backed secrets, mounted under `/run/secrets/…`.
   - Pros: standard pattern for many images.
   - Cons: still backed by files on disk; needs conventions.
4. **External secret manager (PRO, planned)**
   - Vault / SOPS / cloud secret managers.
   - Pros: real secret lifecycle.
   - Cons: more dependencies and operational complexity.

## Recommendations (for now)

- Community v1: prefer public HTTPS repositories (no credentials).
- For private repositories (PRO, planned): prefer deploy keys over broad tokens when possible.
- Store secrets only via the parameters store (never in `docker-compose.yaml` committed to Git).
