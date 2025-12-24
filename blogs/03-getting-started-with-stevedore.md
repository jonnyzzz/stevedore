---
title: "Tutorial: Deploying Your First App with Stevedore"
date: 2025-12-24
---

Ready to turn your Raspberry Pi into a GitOps powerhouse? Let's install Stevedore and deploy a simple web server.

## Prerequisites

*   A Linux host (Ubuntu or Raspberry Pi OS).
*   Docker installed (`curl -fsSL https://get.docker.com | sh`).
*   A GitHub account.

## Step 1: Fork and Install

First, fork the [Stevedore repository](https://github.com/jonnyzzz/stevedore). This is your personal control plane.

SSH into your server and run:

```bash
git clone https://github.com/<YOUR_USERNAME>/stevedore.git
cd stevedore
./stevedore-install.sh
```

This script does a few things:
1.  Builds the Stevedore image.
2.  Sets up the `/opt/stevedore` directory.
3.  Installs a systemd service (`stevedore.service`) so it runs at boot.
4.  Generates your `admin.key` and `db.key`.

Verify it's running:

```bash
stevedore doctor
```

## Step 2: Add a Repository

Let's say you have a repository `my-web-app` with a `docker-compose.yaml`.

Tell Stevedore to watch it:

```bash
stevedore repo add my-app git@github.com:<YOUR_USERNAME>/my-web-app.git
```

Stevedore will generate a unique SSH Key for this deployment. View it:

```bash
stevedore repo key my-app
```

Copy that key. Go to your GitHub repo -> **Settings** -> **Deploy keys** -> **Add deploy key**. Paste it there. This gives Stevedore read-only access to just that repo.

## Step 3: Deploy!

Now, kick off the first sync:

```bash
stevedore deploy sync my-app
```

Stevedore will pull the code. Now, bring it up:

```bash
stevedore deploy up my-app
```

Boom. Your app is running.

## Step 4: The Magic

Here is the best part. Make a change to your `my-web-app` on your laptop. Change the HTML title.

```bash
git commit -am "Update title"
git push
```

Wait a few minutes (Stevedore polls every 5 minutes by default). Refresh your browser.

**It updated automatically.**

You can check the status at any time:

```bash
stevedore status my-app
```

## Managing Secrets

Need to set an API key? Don't commit it.

```bash
stevedore param set my-app API_KEY "super-secret-value"
```

Stevedore stores this in its encrypted database. We are currently building the mechanism to inject these securely into your containers (stay tuned!).

## Conclusion

It's that simple. No Kubernetes manifests. No complex CI/CD pipelines. Just Git, Docker, and Stevedore keeping watch.

Give it a try and let me know what you think!
