---
title: "Under the Hood: How Stevedore Works"
date: 2025-12-24
---

In my [previous post](./01-introducing-stevedore.md), I introduced Stevedore, my lightweight alternative to Kubernetes for small servers. Today, I want to dive into the architecture. How do we build a robust deployment system in Go without reinventing the wheel?

## The Loop

At its heart, Stevedore is a polling loop. It checks your Git repositories every few minutes. But we don't just run `git pull` on the host. That's messy.

### Worker Containers

We follow the "Docker-first" philosophy. The Stevedore daemon itself is a minimal container. When it needs to perform complex state changes or isolated tasks, it spawns a **Worker Container**.

For example, when Stevedore updates itself, it spawns an "Update Worker" that stops the old daemon and starts the new one. It's like a brain transplant, performed by a robot arm.

The architecture also supports running Git operations inside ephemeral `alpine/git` containers to isolate keys and credentials, though for performance we currently default to using the git binary available in the Stevedore image.

## State on Disk

I am tired of distributed key-value stores. Stevedore runs on *one* node. We don't need etcd.

All state lives in `/opt/stevedore`.
*   `/deployments`: Your code and data.
*   `/system`: The internal database.

We use **SQLite**. It's rock solid. But storing secrets (like API keys) in plain text is a bad idea, even on a private server. So we use **SQLCipher**.

When you install Stevedore, it generates a `db.key` file. This key encrypts the SQLite database. Your secrets are safe at rest. It's a simple, pragmatic trade-off. We lose high availability (if the disk dies, we die), but we gain immense simplicity.

## Docker Compose as the Spec

I didn't want to create a `stevedore.yaml` format. Docker Compose is already the industry standard for defining multi-container applications.

Stevedore looks for `docker-compose.yaml` in your repo. It respects standard features like `healthcheck`. If your container says it's healthy, Stevedore is happy. If not, we (eventually) roll back.

We also inject some magic environment variables like `${STEVEDORE_DATA}`. This maps to a persistent volume on the host, so your app doesn't lose data when it redeploys.

## Go + Docker SDK

The code is written in Go. It's robust, statically typed, and has excellent libraries. We use the official Docker SDK to talk to the Docker daemon. This means Stevedore isn't just shelling out to `docker` CLI commands (mostly). We speak the API.

Check out `internal/stevedore/git_worker.go` to see how we manage the ephemeral containers. It's a fun read if you're into system programming!
