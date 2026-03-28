package stevedore

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GitWorkerConfig holds configuration for running git operations in a worker container.
type GitWorkerConfig struct {
	// Deployment name
	Deployment string
	// Docker image to use for git operations
	Image string
	// Timeout for git operations
	Timeout time.Duration
}

// DefaultGitWorkerConfig returns the default configuration for git worker.
func DefaultGitWorkerConfig() GitWorkerConfig {
	return GitWorkerConfig{
		Image:   "alpine/git:latest",
		Timeout: 5 * time.Minute,
	}
}

// GitCloneResult holds the result of a git clone operation.
type GitCloneResult struct {
	// Commit SHA of the cloned repository
	Commit string
	// Branch that was checked out
	Branch string
	// RemovedFiles lists files that were removed during a clean sync
	RemovedFiles []string
}

// GitCheckResult holds the result of a git check operation.
// This is returned by GitCheckRemote which checks for updates without modifying files.
type GitCheckResult struct {
	// CurrentCommit is the commit SHA currently checked out on disk
	CurrentCommit string
	// RemoteCommit is the commit SHA on the remote branch
	RemoteCommit string
	// HasChanges is true if the remote has new commits
	HasChanges bool
	// Branch is the branch being tracked
	Branch string
}

// gitRepoSetup holds the resolved paths and metadata for a git operation.
type gitRepoSetup struct {
	deploymentDir  string
	repoDir        string
	gitDir         string
	sshDir         string
	privateKeyPath string
	repoURL        string
	branch         string
	isClone        bool
}

// prepareGitRepo validates and prepares paths for a git operation.
func (i *Instance) prepareGitRepo(deployment string) (*gitRepoSetup, error) {
	if err := ValidateDeploymentName(deployment); err != nil {
		return nil, err
	}

	deploymentDir := i.DeploymentDir(deployment)
	repoDir := filepath.Join(deploymentDir, "repo")
	gitDir := filepath.Join(repoDir, "git")
	sshDir := filepath.Join(repoDir, "ssh")

	// Read repository URL and branch
	urlBytes, err := os.ReadFile(filepath.Join(repoDir, "url.txt"))
	if err != nil {
		return nil, fmt.Errorf("failed to read repository URL: %w", err)
	}
	repoURL := strings.TrimSpace(string(urlBytes))

	branchBytes, err := os.ReadFile(filepath.Join(repoDir, "branch.txt"))
	if err != nil {
		return nil, fmt.Errorf("failed to read branch: %w", err)
	}
	branch := strings.TrimSpace(string(branchBytes))

	// Check if SSH key exists
	privateKeyPath := filepath.Join(sshDir, "id_ed25519")
	if _, err := os.Stat(privateKeyPath); err != nil {
		return nil, fmt.Errorf("SSH key not found: %w", err)
	}

	// Ensure git directory exists
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create git directory: %w", err)
	}

	// Determine if we need to clone or fetch
	isClone := true
	if _, err := os.Stat(filepath.Join(gitDir, ".git")); err == nil {
		isClone = false
	}

	return &gitRepoSetup{
		deploymentDir:  deploymentDir,
		repoDir:        repoDir,
		gitDir:         gitDir,
		sshDir:         sshDir,
		privateKeyPath: privateKeyPath,
		repoURL:        repoURL,
		branch:         branch,
		isClone:        isClone,
	}, nil
}

// runGitScript runs a shell script in a git worker container.
// The script is executed after SSH keys are configured. The repo is mounted at /repo.
// Returns the stdout output.
func (i *Instance) runGitScript(ctx context.Context, deployment string, script string) (string, error) {
	setup, err := i.prepareGitRepo(deployment)
	if err != nil {
		return "", err
	}

	fullScript := fmt.Sprintf(`set -e
mkdir -p ~/.ssh
cp /ssh-keys/id_ed25519 ~/.ssh/id_ed25519
chmod 600 ~/.ssh/id_ed25519
ssh-keyscan -t ed25519 github.com >> ~/.ssh/known_hosts 2>/dev/null || true
ssh-keyscan -t ed25519 gitlab.com >> ~/.ssh/known_hosts 2>/dev/null || true
ssh-keyscan -t ed25519 bitbucket.org >> ~/.ssh/known_hosts 2>/dev/null || true
export GIT_SSH_COMMAND="ssh -o StrictHostKeyChecking=accept-new -i ~/.ssh/id_ed25519"
cd /repo
%s
`, script)

	image := DefaultGitWorkerConfig().Image
	containerName := fmt.Sprintf("stevedore-git-%s-%d", deployment, time.Now().UnixNano())

	args := []string{
		"run",
		"--rm",
		"--name", containerName,
		"--entrypoint", "sh",
		"--label", "com.stevedore.managed=true",
		"--label", "com.stevedore.deployment=" + deployment,
		"--label", "com.stevedore.role=git-worker",
		"-v", setup.sshDir + ":/ssh-keys:ro",
		"-v", setup.gitDir + ":/repo",
		image,
		"-c", fullScript,
	}

	cmd := newCommand(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
}

// GitCheckRemote checks for updates by fetching from the remote in a worker container.
// This is safe to call while the deployment is running — it only updates refs via fetch,
// without modifying the working directory.
func (i *Instance) GitCheckRemote(ctx context.Context, deployment string) (*GitCheckResult, error) {
	setup, err := i.prepareGitRepo(deployment)
	if err != nil {
		return nil, err
	}

	// If repo doesn't exist yet, there's no current commit
	if setup.isClone {
		return &GitCheckResult{
			CurrentCommit: "",
			RemoteCommit:  "",
			HasChanges:    true,
			Branch:        setup.branch,
		}, nil
	}

	script := fmt.Sprintf(`
CURRENT=$(git rev-parse HEAD)
git fetch --depth 1 origin %s
REMOTE=$(git rev-parse FETCH_HEAD)
echo "STEVEDORE_CURRENT=$CURRENT"
echo "STEVEDORE_REMOTE=$REMOTE"
`, setup.branch)

	output, err := i.runGitScript(ctx, deployment, script)
	if err != nil {
		return nil, fmt.Errorf("git check remote failed: %w", err)
	}

	var currentCommit, remoteCommit string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "STEVEDORE_CURRENT=") {
			currentCommit = strings.TrimPrefix(line, "STEVEDORE_CURRENT=")
		}
		if strings.HasPrefix(line, "STEVEDORE_REMOTE=") {
			remoteCommit = strings.TrimPrefix(line, "STEVEDORE_REMOTE=")
		}
	}

	return &GitCheckResult{
		CurrentCommit: currentCommit,
		RemoteCommit:  remoteCommit,
		HasChanges:    currentCommit != remoteCommit,
		Branch:        setup.branch,
	}, nil
}

// GitSyncClean performs a git clone or fetch+reset in a worker container,
// and removes stale/untracked files. All git and ssh processes are isolated
// inside the container and cleaned up when it exits.
func (i *Instance) GitSyncClean(ctx context.Context, deployment string, cleanEnabled bool) (*GitCloneResult, error) {
	setup, err := i.prepareGitRepo(deployment)
	if err != nil {
		return nil, err
	}

	var script string
	if setup.isClone {
		script = fmt.Sprintf(`
git clone --branch %s --depth 1 --single-branch %s .
echo "STEVEDORE_COMMIT=$(git rev-parse HEAD)"
`, setup.branch, setup.repoURL)
	} else if cleanEnabled {
		script = fmt.Sprintf(`
git fetch --depth 1 origin %s
git reset --hard FETCH_HEAD
CLEAN_OUTPUT=$(git clean -fd 2>/dev/null || true)
if [ -n "$CLEAN_OUTPUT" ]; then
  echo "$CLEAN_OUTPUT" | while IFS= read -r line; do
    echo "STEVEDORE_CLEAN=$line"
  done
fi
echo "STEVEDORE_COMMIT=$(git rev-parse HEAD)"
`, setup.branch)
	} else {
		script = fmt.Sprintf(`
git fetch --depth 1 origin %s
git reset --hard FETCH_HEAD
echo "STEVEDORE_COMMIT=$(git rev-parse HEAD)"
`, setup.branch)
	}

	output, err := i.runGitScript(ctx, deployment, script)
	if err != nil {
		return nil, fmt.Errorf("git sync failed: %w", err)
	}

	var commit string
	var removedFiles []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "STEVEDORE_COMMIT=") {
			commit = strings.TrimPrefix(line, "STEVEDORE_COMMIT=")
		}
		if strings.HasPrefix(line, "STEVEDORE_CLEAN=") {
			cleaned := strings.TrimPrefix(line, "STEVEDORE_CLEAN=")
			if strings.HasPrefix(cleaned, "Removing ") {
				f := strings.TrimPrefix(cleaned, "Removing ")
				removedFiles = append(removedFiles, f)
				log.Printf("Removed untracked: %s", f)
			}
		}
	}

	if commit == "" {
		return nil, fmt.Errorf("git sync did not return commit SHA")
	}

	return &GitCloneResult{
		Commit:       commit,
		Branch:       setup.branch,
		RemovedFiles: removedFiles,
	}, nil
}
