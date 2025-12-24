# Implementation Plan: Repository Monitoring Service

## Overview

This plan implements:
1. Repository monitoring service with `git fetch` check (no file modification)
2. CLI-to-daemon communication via HTTP API with version verification
3. Clean update mechanism that removes stale files
4. Full integration test for the deployment workflow

## Current State Analysis

### Existing Infrastructure
- **Daemon** (`internal/stevedore/daemon.go`): Polling loop exists, syncs on configurable intervals
- **HTTP Server** (`internal/stevedore/server.go`): Token-based auth, endpoints for sync/deploy/status
- **Git Operations** (`internal/stevedore/git_worker.go`): `GitCloneLocal()` does fetch + checkout (modifies files)

### Gap Analysis
1. No "check-only" operation (fetch without checkout)
2. No CLI command to trigger immediate check via daemon
3. No version/binary hash verification header
4. No stale file cleanup during updates
5. No integration test for full workflow

## Implementation Plan

### Phase 1: Git Check-Only Operation

**File:** `internal/stevedore/git_worker.go`

Add new method that only fetches and compares commits without modifying working directory:

```go
// GitCheckResult holds the result of a git check operation.
type GitCheckResult struct {
    CurrentCommit  string // Commit on disk
    RemoteCommit   string // Commit on remote
    HasChanges     bool   // True if remote is ahead
    Branch         string
}

// GitCheckRemote performs a git fetch to check for updates without modifying files.
// This is safe to call while the deployment is running.
func (i *Instance) GitCheckRemote(ctx context.Context, deployment string) (*GitCheckResult, error)
```

Implementation approach:
1. Read current HEAD commit from local `.git`
2. Run `git fetch origin <branch>` (updates refs but not files)
3. Run `git rev-parse FETCH_HEAD` to get remote commit
4. Compare commits to detect changes

### Phase 2: Clean Update with Stale File Removal

**File:** `internal/stevedore/git_worker.go`

Enhance sync to:
1. Before checkout, get list of tracked files
2. After checkout, compare with new tracked files
3. Remove files that are no longer tracked
4. Log all removed files

```go
// GitSyncClean performs a git sync and removes stale untracked files.
// Returns list of files that were removed.
func (i *Instance) GitSyncClean(ctx context.Context, deployment string) (*GitCloneResult, []string, error)
```

Commands:
```bash
# Get list of files before update
git ls-tree -r --name-only HEAD > /tmp/before.txt

# Fetch and hard reset (discards local changes)
git fetch origin <branch>
git reset --hard FETCH_HEAD
git clean -fd  # Remove untracked files

# Log what was cleaned
```

### Phase 3: Version Header for CLI-Daemon Communication

**File:** `internal/stevedore/server.go`

Add version verification:

```go
const (
    HeaderStevedoreVersion = "X-Stevedore-Version"
    HeaderStevedoreBuild   = "X-Stevedore-Build"
)

// Version header format: "version:build_hash"
// Server validates client version matches
```

Client sends:
- `X-Stevedore-Version: 0.1.0`
- `X-Stevedore-Build: abc123` (binary hash or git commit)

Server validates and warns if mismatch (but doesn't reject - allows upgrade scenarios).

### Phase 4: HTTP API Endpoint for Check

**File:** `internal/stevedore/server.go`

Add new endpoint:

```go
// POST /api/check/{name} - Check for updates without deploying
// Response:
// {
//   "deployment": "myapp",
//   "currentCommit": "abc123",
//   "remoteCommit": "def456",
//   "hasChanges": true,
//   "branch": "main"
// }
mux.HandleFunc("/api/check/", s.requireAuth(s.handleAPICheck))
```

### Phase 5: CLI Command to Trigger Check

**File:** `main.go` (or new `internal/stevedore/client.go`)

Add CLI command:

```bash
# Check single deployment
stevedore check <deployment>

# Check all deployments
stevedore check --all

# Trigger sync if changes detected
stevedore check <deployment> --sync
```

Implementation:
```go
// Client wraps HTTP calls to the daemon
type Client struct {
    BaseURL  string
    AdminKey string
    Version  string
    Build    string
}

func (c *Client) Check(ctx context.Context, deployment string) (*GitCheckResult, error)
func (c *Client) Sync(ctx context.Context, deployment string) (*GitCloneResult, error)
```

### Phase 6: Daemon Integration

**File:** `internal/stevedore/daemon.go`

Update poll loop to use check-only first:

```go
func (d *Daemon) pollDeployment(ctx context.Context, deployment string) {
    // 1. Check for changes (git fetch only)
    check, err := d.instance.GitCheckRemote(ctx, deployment)
    if err != nil {
        log.Printf("Check failed for %s: %v", deployment, err)
        return
    }

    if !check.HasChanges {
        return // No changes, skip
    }

    // 2. Changes detected, sync and deploy
    log.Printf("Changes detected for %s: %s -> %s",
        deployment, shortCommit(check.CurrentCommit), shortCommit(check.RemoteCommit))

    d.syncAndDeploy(ctx, deployment)
}
```

### Phase 7: Integration Test

**File:** `tests/integration/monitoring_test.go`

Full workflow test:

```go
func TestMonitoringWorkflow(t *testing.T) {
    // 1. Set up test containers
    donor := NewTestContainer(t, "Dockerfile.ubuntu")
    gitServer := NewGitServer(t)

    // 2. Install stevedore
    donor.CopySourcesToWorkDir("/work/stevedore")
    donor.ExecBashOKTimeout(env, "cd /work/stevedore && ./stevedore-install.sh", 10*time.Minute)

    // 3. Create test repo with initial content
    gitServer.InitRepoWithContent("test-app", map[string]string{
        "docker-compose.yaml": composeContent,
        "version.txt": "v1.0.0",
    })

    // 4. Add deployment to stevedore
    pubKey := donor.ExecEnvOK(env, "stevedore", "repo", "add", "test-app", gitURL, "--branch", "main")
    gitServer.AddAuthorizedKey(pubKey)

    // 5. Initial sync and deploy
    donor.ExecEnvOK(env, "stevedore", "deploy", "sync", "test-app")
    donor.ExecEnvOK(env, "stevedore", "deploy", "up", "test-app")

    // 6. Verify deployment is running
    waitForHealthy(t, donor, env, "test-app", 60*time.Second)

    // 7. Make changes to git repo
    gitServer.UpdateFile("test-app", "version.txt", "v2.0.0")
    gitServer.Commit("test-app", "Update to v2.0.0")

    // 8. Check for changes via CLI
    checkOutput := donor.ExecEnvOK(env, "stevedore", "check", "test-app")
    if !strings.Contains(checkOutput, "hasChanges: true") {
        t.Fatal("Expected changes to be detected")
    }

    // 9. Wait for automatic redeploy (or trigger manually)
    // Option A: Wait for daemon poll
    // Option B: Trigger via stevedore check --sync
    donor.ExecEnvOK(env, "stevedore", "check", "test-app", "--sync")

    // 10. Verify new version is deployed
    waitForHealthy(t, donor, env, "test-app", 60*time.Second)
    version := getDeployedVersion(t, donor, "test-app")
    if version != "v2.0.0" {
        t.Fatalf("Expected v2.0.0, got %s", version)
    }

    // 11. Cleanup
    donor.ExecEnvOK(env, "stevedore", "deploy", "down", "test-app")
}
```

### Phase 8: Git Client Component Extraction

**File:** `internal/stevedore/gitclient/client.go`

Extract git operations into a separate, testable package:

```go
package gitclient

// Client provides git operations via Docker containers
type Client struct {
    DockerHost string
    Image      string // default: alpine/git:latest
}

// CloneOptions for clone operation
type CloneOptions struct {
    URL     string
    Branch  string
    SSHKey  string
    WorkDir string
    Shallow bool
}

// Clone clones a repository
func (c *Client) Clone(ctx context.Context, opts CloneOptions) error

// Fetch fetches from remote
func (c *Client) Fetch(ctx context.Context, workDir, remote, branch string) error

// GetCommit returns the current HEAD commit
func (c *Client) GetCommit(ctx context.Context, workDir string) (string, error)

// GetRemoteCommit returns the FETCH_HEAD commit after fetch
func (c *Client) GetRemoteCommit(ctx context.Context, workDir string) (string, error)

// Reset performs a hard reset to a commit
func (c *Client) Reset(ctx context.Context, workDir, commit string) error

// Clean removes untracked files
func (c *Client) Clean(ctx context.Context, workDir string) ([]string, error)
```

**Test file:** `internal/stevedore/gitclient/client_test.go`

Unit tests using the existing GitServer helper:

```go
func TestClient_Clone(t *testing.T) {
    gs := integration.NewGitServer(t)
    client := gitclient.New()
    // ...
}
```

## File Changes Summary

| File | Action | Description |
|------|--------|-------------|
| `internal/stevedore/git_worker.go` | Modify | Add `GitCheckRemote()`, `GitSyncClean()` |
| `internal/stevedore/server.go` | Modify | Add `/api/check/`, version headers |
| `internal/stevedore/daemon.go` | Modify | Use check-only in poll loop |
| `internal/stevedore/client.go` | Create | HTTP client for CLI-daemon communication |
| `main.go` | Modify | Add `check` command |
| `internal/stevedore/gitclient/client.go` | Create | Extracted git client component |
| `internal/stevedore/gitclient/client_test.go` | Create | Git client unit tests |
| `tests/integration/monitoring_test.go` | Create | Full workflow integration test |
| `docs/ARCHITECTURE.md` | Update | Document monitoring service |

## Implementation Order

1. **Git check-only operation** (git_worker.go) - Core functionality
2. **HTTP endpoint** (server.go) - API for triggering check
3. **Version headers** (server.go) - Security/compatibility
4. **HTTP client** (client.go) - CLI needs this
5. **CLI command** (main.go) - User-facing interface
6. **Daemon integration** (daemon.go) - Wire it together
7. **Clean sync** (git_worker.go) - Stale file removal
8. **Git client extraction** (gitclient/) - Testability
9. **Integration test** (monitoring_test.go) - Validation
10. **Documentation** (*.md) - Update docs

## Success Criteria

1. `stevedore check <deployment>` returns current vs remote commit comparison
2. Daemon uses `git fetch` only during polling (no file modification)
3. When changes detected, full sync with stale file cleanup
4. CLI-daemon communication includes version verification
5. Integration test passes covering full workflow
6. Git client component is independently testable

## Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Git fetch may timeout on large repos | Use shallow fetch with `--depth 1` |
| Stale file removal may break running services | Only clean on explicit sync/deploy |
| Version mismatch between CLI and daemon | Warn but don't block (allows upgrades) |
| Concurrent syncs may conflict | Existing mutex protection in daemon |

## Questions to Clarify

1. Should the check command wait for result or return immediately?
   - **Recommendation**: Wait for result (synchronous) for CLI, async for daemon poll

2. Should stale file removal be configurable?
   - **Recommendation**: Default on, with `--no-clean` flag for sync

3. Should version mismatch reject requests?
   - **Recommendation**: Warn only, allow minor version differences
