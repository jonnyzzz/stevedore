# Repositories (Deployments)

Stevedore manages deployments by polling Git repositories. The recommended onboarding flow is SSH
deploy keys (read-only).

## Add a Repository

```bash
stevedore.sh repo add <deployment> <git-url> --branch <branch>
```

`--branch` defaults to `main` if omitted.

Example:

```bash
stevedore.sh repo add homepage git@github.com:acme/homepage.git --branch main
```

This creates the deployment state directory and generates an SSH keypair.

## Get the Public Deploy Key

```bash
stevedore.sh repo key <deployment>
```

Add the printed public key to the Git hosting provider as a **read-only deploy key**.

### GitHub UI

1. Open repository → **Settings**
2. **Deploy keys** → **Add deploy key**
3. Paste the public key
4. Keep **Allow write access** unchecked

## Where the Keys Live

By default:

- Private key: `/opt/stevedore/deployments/<deployment>/repo/ssh/id_ed25519`
- Public key: `/opt/stevedore/deployments/<deployment>/repo/ssh/id_ed25519.pub`

## Alternatives (Options)

- **HTTPS + token**: simpler for some setups, but tokens are broader than deploy keys.
- **SSH agent forwarding**: convenient for dev, but harder to make reproducible.

## Compose Entrypoint

Each repository must have a Compose file at repo root. Preferred filename: `docker-compose.yaml`.

Stevedore also accepts: `docker-compose.yml`, `compose.yaml`, `compose.yml` (and `stevedore.yaml` as legacy).
