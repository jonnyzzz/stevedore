---
title: "Stevedore: GitOps for your Raspberry Pi"
date: 2025-12-24
---

I love my Raspberry Pi. It runs my home automation, a few discord bots, and this very blog. But managing it has always been a bit of a pain.

I've tried Kubernetes (k3s). It's amazing, but the overhead is real. Suddenly my 4GB Pi is eating 1GB just to exist. I've tried shell scripts via SSH. It works until I forget which script I ran last. I've tried Portainer, but I wanted something that felt more "native" to my workflow: **Git push**.

I wanted the Heroku experience, but on my own hardware. I wanted to push code to GitHub and have my Pi update itself. No CI pipelines to configure, no complex Terraform state to manage. Just a simple loop:

1.  Is there new code?
2.  `git pull`
3.  `docker compose up -d`

So I built **Stevedore**.

## What is Stevedore?

Stevedore is a lightweight, self-managing container orchestration system. It runs as a single container on your host (Ubuntu or Raspberry Pi OS). You tell it which Git repositories to watch, and it takes care of the rest.

It's designed to be:
*   **Boring:** It uses Docker Compose. If you know Docker, you know Stevedore.
*   **Secure-ish:** It generates SSH deploy keys for your repos. Secrets are encrypted on disk.
*   **Self-healing:** The control plane runs as a systemd service; workload restarts follow your Compose restart policy.

## Why not just a cron job?

You could write a bash script that does `git pull && docker compose up`. I did that for years. But then you hit edge cases:
*   What if `docker compose build` fails? You don't want to take down the running app.
*   How do you manage secrets without committing `.env` files?
*   How do you update the script itself?

Stevedore handles these gracefully. It runs builds using standard Docker Compose, ensuring consistency. It has a dedicated encrypted parameter store. And yes, it can update itself.

## The "Fork First" Philosophy

Stevedore is open source, but it's designed to be *your* infrastructure. The installation process encourages you to fork the repository first. This gives you control. If I break something in the upstream `main`, your house doesn't burn down. You pull updates when you are ready.

## Coming Next

I'm still actively building it. The core "sync -> build -> deploy" loop is working. I'm adding a web UI (because who doesn't love a dashboard?) and hardening the security.

Check it out on [GitHub](https://github.com/jonnyzzz/stevedore). If you have a Raspberry Pi collecting dust, give it a job!
