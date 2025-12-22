package stevedore

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
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
}

// GitSync performs a git clone or pull operation for a deployment using a worker container.
// It clones if the repo doesn't exist, or fetches and checks out if it does.
func (i *Instance) GitSync(ctx context.Context, deployment string, config GitWorkerConfig) (*GitCloneResult, error) {
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

	if config.Timeout == 0 {
		config.Timeout = DefaultGitWorkerConfig().Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	if isClone {
		// Clone the repository
		if err := i.runGitWorker(ctx, deployment, config, []string{
			"clone",
			"--branch", branch,
			"--depth", "1",
			"--single-branch",
			repoURL,
			".",
		}, gitDir); err != nil {
			return nil, fmt.Errorf("git clone failed: %w", err)
		}
	} else {
		// Fetch and checkout
		if err := i.runGitWorker(ctx, deployment, config, []string{
			"fetch", "--depth", "1", "origin", branch,
		}, gitDir); err != nil {
			return nil, fmt.Errorf("git fetch failed: %w", err)
		}

		if err := i.runGitWorker(ctx, deployment, config, []string{
			"checkout", "-f", "FETCH_HEAD",
		}, gitDir); err != nil {
			return nil, fmt.Errorf("git checkout failed: %w", err)
		}
	}

	// Get the current commit SHA
	commit, err := i.getGitCommit(ctx, deployment, config, gitDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	return &GitCloneResult{
		Commit: commit,
		Branch: branch,
	}, nil
}

// runGitWorker runs a git command in a worker container.
func (i *Instance) runGitWorker(ctx context.Context, deployment string, config GitWorkerConfig, gitArgs []string, workDir string) error {
	sshDir := filepath.Join(i.DeploymentDir(deployment), "repo", "ssh")

	// Build the docker run command
	// We use a script to set up SSH properly
	gitScript := fmt.Sprintf(`
set -e
mkdir -p ~/.ssh
cp /ssh-keys/id_ed25519 ~/.ssh/id_ed25519
chmod 600 ~/.ssh/id_ed25519
ssh-keyscan -t ed25519 github.com >> ~/.ssh/known_hosts 2>/dev/null || true
ssh-keyscan -t ed25519 gitlab.com >> ~/.ssh/known_hosts 2>/dev/null || true
ssh-keyscan -t ed25519 bitbucket.org >> ~/.ssh/known_hosts 2>/dev/null || true
export GIT_SSH_COMMAND="ssh -o StrictHostKeyChecking=accept-new -i ~/.ssh/id_ed25519"
cd /repo
git %s
`, strings.Join(gitArgs, " "))

	image := config.Image
	if image == "" {
		image = DefaultGitWorkerConfig().Image
	}

	containerName := fmt.Sprintf("stevedore-git-%s-%d", deployment, time.Now().UnixNano())

	args := []string{
		"run",
		"--rm",
		"--name", containerName,
		"--label", "com.stevedore.managed=true",
		"--label", "com.stevedore.deployment=" + deployment,
		"--label", "com.stevedore.role=git-worker",
		"-v", sshDir + ":/ssh-keys:ro",
		"-v", workDir + ":/repo",
		image,
		"sh", "-c", gitScript,
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return nil
}

// getGitCommit retrieves the current HEAD commit SHA.
func (i *Instance) getGitCommit(ctx context.Context, deployment string, config GitWorkerConfig, workDir string) (string, error) {
	image := config.Image
	if image == "" {
		image = DefaultGitWorkerConfig().Image
	}

	containerName := fmt.Sprintf("stevedore-git-%s-%d", deployment, time.Now().UnixNano())

	args := []string{
		"run",
		"--rm",
		"--name", containerName,
		"--label", "com.stevedore.managed=true",
		"--label", "com.stevedore.deployment=" + deployment,
		"--label", "com.stevedore.role=git-worker",
		"-v", workDir + ":/repo",
		image,
		"git", "-C", "/repo", "rev-parse", "HEAD",
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}

// GitCloneLocal performs a git clone using the local git binary (no worker container).
// This is useful for environments where docker-in-docker is not available.
func (i *Instance) GitCloneLocal(ctx context.Context, deployment string) (*GitCloneResult, error) {
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

	// Set up SSH command environment
	sshCommand := fmt.Sprintf("ssh -o StrictHostKeyChecking=accept-new -i %s", privateKeyPath)

	// Determine if we need to clone or fetch
	isClone := true
	if _, err := os.Stat(filepath.Join(gitDir, ".git")); err == nil {
		isClone = false
	}

	var cmd *exec.Cmd
	if isClone {
		cmd = exec.CommandContext(ctx, "git", "clone", "--branch", branch, "--depth", "1", "--single-branch", repoURL, gitDir)
	} else {
		// First fetch
		fetchCmd := exec.CommandContext(ctx, "git", "-C", gitDir, "fetch", "--depth", "1", "origin", branch)
		fetchCmd.Env = append(os.Environ(), "GIT_SSH_COMMAND="+sshCommand)
		var fetchStderr bytes.Buffer
		fetchCmd.Stderr = &fetchStderr
		if err := fetchCmd.Run(); err != nil {
			return nil, fmt.Errorf("git fetch failed: %w: %s", err, strings.TrimSpace(fetchStderr.String()))
		}

		// Then checkout
		cmd = exec.CommandContext(ctx, "git", "-C", gitDir, "checkout", "-f", "FETCH_HEAD")
	}

	cmd.Env = append(os.Environ(), "GIT_SSH_COMMAND="+sshCommand)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git operation failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	// Get commit SHA
	commitCmd := exec.CommandContext(ctx, "git", "-C", gitDir, "rev-parse", "HEAD")
	commitOut, err := commitCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	return &GitCloneResult{
		Commit: strings.TrimSpace(string(commitOut)),
		Branch: branch,
	}, nil
}
