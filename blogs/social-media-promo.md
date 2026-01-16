# Social Media Promo Content

## Twitter / X (Thread)

**Post 1:**
Tired of manually SSH-ing into your Raspberry Pi to `git pull && docker compose up`? 😫

I built Stevedore: A "boring" GitOps tool for your homelab.
No Kubernetes overhead. No complex CI. Just Git, Docker, and a single Go binary.

https://github.com/jonnyzzz/stevedore

🧵👇

**Post 2:**
Stevedore runs as a single container.
1. You `git push` to GitHub.
2. Stevedore sees the change.
3. It runs `docker compose up -d` for you.

It handles build failures, secrets (encrypted on disk), and even updates itself.

**Post 3:**
Why not just a cron job?
- Atomic updates (don't kill the app if the build fails)
- Secret management (SQLCipher built-in)
- Git operations with per-repo deploy keys
- It's actually observable (`stevedore status`)

**Post 4:**
It's designed for the "Fork First" philosophy.
You don't just install it; you fork it. It becomes *your* infrastructure.
If I break upstream, your house doesn't burn down.

**Post 5:**
Read the full story and how to set it up on your Pi in 5 minutes:
https://jonnyzzz.com/blog/2025/12/24/introducing-stevedore/

And the tutorial:
https://jonnyzzz.com/blog/2025/12/24/getting-started-with-stevedore/

#homelab #selfhosted #gitops #golang #raspberrypi

---

### 🖼️ Image Suggestions for Twitter

1.  **Hero Image:** A clean photo of a Raspberry Pi sitting on a desk, with a terminal window overlay showing the `stevedore status` command output. Green checkmarks everywhere.
2.  **Diagram:** A simplified version of the architecture diagram (GitHub -> Stevedore (on Pi) -> App Container).
3.  **Terminal GIF:** A GIF showing `stevedore repo add ...` followed by `stevedore deploy sync`.

---

## LinkedIn

**Headline:** GitOps for the rest of us. 🚢

**Body:**
I love Kubernetes, but running k3s on a 4GB Raspberry Pi just to host a simple discord bot felt like overkill. The overhead was eating up half my RAM.

On the other hand, hacking together bash scripts to `git pull` on a cron job is fragile. What happens if the build fails? What about secrets?

So I built **Stevedore**.

It's a lightweight, self-managing container orchestration system designed specifically for small hardware and homelabs.

✅ **Boring tech:** It uses standard Docker Compose.
✅ **Secure:** Secrets are encrypted at rest using SQLCipher.
✅ **Self-healing:** The control plane runs as a systemd service; workload restarts follow your Compose restart policy.
✅ **Zero-config CI:** Your `git push` is the trigger.

It follows a "Fork First" philosophy. You fork the repo, and that fork becomes your control plane. It puts you in complete control of your infrastructure updates.

I wrote a deep dive into why I built it and how it works under the hood.

👉 Read the intro: https://jonnyzzz.com/blog/2025/12/24/introducing-stevedore/
👉 Read the architecture deep dive: https://jonnyzzz.com/blog/2025/12/24/stevedore-architecture/

#devops #gitops #selfhosting #homelab #golang #docker #opensource

---

### 🖼️ Image Suggestions for LinkedIn

1.  **Architecture Diagram:** A polished, high-contrast diagram showing the flow from "Git Push" to "Live Container".
2.  **Comparison Chart:** A simple table comparing "Manual SSH", "Kubernetes (K3s)", and "Stevedore".
    *   *Columns:* RAM Usage, Complexity, Setup Time.
    *   *Stevedore* should highlight "Low", "Low", "5 mins".
