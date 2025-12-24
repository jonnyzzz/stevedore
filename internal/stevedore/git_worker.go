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

// GitSync performs a git clone or pull operation for a deployment using a worker container.
// It clones if the repo doesn't exist, or fetches and checks out if it does.
func (i *Instance) GitSync(ctx context.Context, deployment string, config GitWorkerConfig) (*GitCloneResult, error) {
	setup, err := i.prepareGitRepo(deployment)
	if err != nil {
		return nil, err
	}

	if config.Timeout == 0 {
		config.Timeout = DefaultGitWorkerConfig().Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	if setup.isClone {
		// Clone the repository
		if err := i.runGitWorker(ctx, deployment, config, []string{
			"clone",
			"--branch", setup.branch,
			"--depth", "1",
			"--single-branch",
			setup.repoURL,
			".",
		}, setup.gitDir); err != nil {
			return nil, fmt.Errorf("git clone failed: %w", err)
		}
	} else {
		// Fetch and checkout
		if err := i.runGitWorker(ctx, deployment, config, []string{
			"fetch", "--depth", "1", "origin", setup.branch,
		}, setup.gitDir); err != nil {
			return nil, fmt.Errorf("git fetch failed: %w", err)
		}

		if err := i.runGitWorker(ctx, deployment, config, []string{
			"checkout", "-f", "FETCH_HEAD",
		}, setup.gitDir); err != nil {
			return nil, fmt.Errorf("git checkout failed: %w", err)
		}
	}

	// Get the current commit SHA
	commit, err := i.getGitCommit(ctx, deployment, config, setup.gitDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	return &GitCloneResult{
		Commit: commit,
		Branch: setup.branch,
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
	setup, err := i.prepareGitRepo(deployment)
	if err != nil {
		return nil, err
	}

	// Set up SSH command environment
	sshCommand := fmt.Sprintf("ssh -o StrictHostKeyChecking=accept-new -i %s", setup.privateKeyPath)

	var cmd *exec.Cmd
	if setup.isClone {
		cmd = exec.CommandContext(ctx, "git", "clone", "--branch", setup.branch, "--depth", "1", "--single-branch", setup.repoURL, setup.gitDir)
	} else {
		// First fetch
		fetchCmd := exec.CommandContext(ctx, "git", "-C", setup.gitDir, "fetch", "--depth", "1", "origin", setup.branch)
		fetchCmd.Env = append(os.Environ(), "GIT_SSH_COMMAND="+sshCommand)
		var fetchStderr bytes.Buffer
		fetchCmd.Stderr = &fetchStderr
		if err := fetchCmd.Run(); err != nil {
			return nil, fmt.Errorf("git fetch failed: %w: %s", err, strings.TrimSpace(fetchStderr.String()))
		}

		// Then checkout
		cmd = exec.CommandContext(ctx, "git", "-C", setup.gitDir, "checkout", "-f", "FETCH_HEAD")
	}

	cmd.Env = append(os.Environ(), "GIT_SSH_COMMAND="+sshCommand)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git operation failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	// Get commit SHA
	commitCmd := exec.CommandContext(ctx, "git", "-C", setup.gitDir, "rev-parse", "HEAD")
	commitOut, err := commitCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	return &GitCloneResult{
		Commit: strings.TrimSpace(string(commitOut)),
		Branch: setup.branch,
	}, nil
}

// GitCheckRemote performs a git fetch to check for updates without modifying the working directory.
// This is safe to call while the deployment is running as it only updates refs, not files.
func (i *Instance) GitCheckRemote(ctx context.Context, deployment string) (*GitCheckResult, error) {
	setup, err := i.prepareGitRepo(deployment)
	if err != nil {
		return nil, err
	}

	// If repo doesn't exist yet, there's no current commit
	if setup.isClone {
		return &GitCheckResult{
			CurrentCommit: "",
			RemoteCommit:  "", // Would need to clone to get this
			HasChanges:    true,
			Branch:        setup.branch,
		}, nil
	}

	// Set up SSH command environment
	sshCommand := fmt.Sprintf("ssh -o StrictHostKeyChecking=accept-new -i %s", setup.privateKeyPath)

	// Get current HEAD commit
	currentCommitCmd := exec.CommandContext(ctx, "git", "-C", setup.gitDir, "rev-parse", "HEAD")
	currentCommitOut, err := currentCommitCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get current commit: %w", err)
	}
	currentCommit := strings.TrimSpace(string(currentCommitOut))

	// Fetch from remote (this only updates refs, not working directory)
	fetchCmd := exec.CommandContext(ctx, "git", "-C", setup.gitDir, "fetch", "--depth", "1", "origin", setup.branch)
	fetchCmd.Env = append(os.Environ(), "GIT_SSH_COMMAND="+sshCommand)
	var fetchStderr bytes.Buffer
	fetchCmd.Stderr = &fetchStderr
	if err := fetchCmd.Run(); err != nil {
		return nil, fmt.Errorf("git fetch failed: %w: %s", err, strings.TrimSpace(fetchStderr.String()))
	}

	// Get FETCH_HEAD commit (what we just fetched)
	remoteCommitCmd := exec.CommandContext(ctx, "git", "-C", setup.gitDir, "rev-parse", "FETCH_HEAD")
	remoteCommitOut, err := remoteCommitCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get remote commit: %w", err)
	}
	remoteCommit := strings.TrimSpace(string(remoteCommitOut))

	return &GitCheckResult{
		CurrentCommit: currentCommit,
		RemoteCommit:  remoteCommit,
		HasChanges:    currentCommit != remoteCommit,
		Branch:        setup.branch,
	}, nil
}

// GitSyncClean performs a git sync and removes stale files that are no longer tracked.
// It logs all removed files and returns them in the result.
func (i *Instance) GitSyncClean(ctx context.Context, deployment string, cleanEnabled bool) (*GitCloneResult, error) {
	setup, err := i.prepareGitRepo(deployment)
	if err != nil {
		return nil, err
	}

	sshCommand := fmt.Sprintf("ssh -o StrictHostKeyChecking=accept-new -i %s", setup.privateKeyPath)

	var removedFiles []string

	if setup.isClone {
		// Clone the repository
		cmd := exec.CommandContext(ctx, "git", "clone", "--branch", setup.branch, "--depth", "1", "--single-branch", setup.repoURL, setup.gitDir)
		cmd.Env = append(os.Environ(), "GIT_SSH_COMMAND="+sshCommand)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("git clone failed: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
	} else {
		// Get list of tracked files before update (for stale file detection)
		var filesBefore map[string]bool
		if cleanEnabled {
			filesBefore = make(map[string]bool)
			lsCmd := exec.CommandContext(ctx, "git", "-C", setup.gitDir, "ls-tree", "-r", "--name-only", "HEAD")
			lsOut, err := lsCmd.Output()
			if err == nil {
				for _, f := range strings.Split(strings.TrimSpace(string(lsOut)), "\n") {
					if f != "" {
						filesBefore[f] = true
					}
				}
			}
		}

		// Fetch
		fetchCmd := exec.CommandContext(ctx, "git", "-C", setup.gitDir, "fetch", "--depth", "1", "origin", setup.branch)
		fetchCmd.Env = append(os.Environ(), "GIT_SSH_COMMAND="+sshCommand)
		var fetchStderr bytes.Buffer
		fetchCmd.Stderr = &fetchStderr
		if err := fetchCmd.Run(); err != nil {
			return nil, fmt.Errorf("git fetch failed: %w: %s", err, strings.TrimSpace(fetchStderr.String()))
		}

		// Hard reset to discard any local changes
		resetCmd := exec.CommandContext(ctx, "git", "-C", setup.gitDir, "reset", "--hard", "FETCH_HEAD")
		var resetStderr bytes.Buffer
		resetCmd.Stderr = &resetStderr
		if err := resetCmd.Run(); err != nil {
			return nil, fmt.Errorf("git reset failed: %w: %s", err, strings.TrimSpace(resetStderr.String()))
		}

		if cleanEnabled {
			// Get list of tracked files after update
			filesAfter := make(map[string]bool)
			lsCmd := exec.CommandContext(ctx, "git", "-C", setup.gitDir, "ls-tree", "-r", "--name-only", "HEAD")
			lsOut, err := lsCmd.Output()
			if err == nil {
				for _, f := range strings.Split(strings.TrimSpace(string(lsOut)), "\n") {
					if f != "" {
						filesAfter[f] = true
					}
				}
			}

			// Find and remove stale files (were tracked before but not after)
			for f := range filesBefore {
				if !filesAfter[f] {
					filePath := filepath.Join(setup.gitDir, f)
					if _, err := os.Stat(filePath); err == nil {
						if err := os.Remove(filePath); err != nil {
							// Log but don't fail on removal errors
							fmt.Printf("Warning: failed to remove stale file %s: %v\n", f, err)
						} else {
							removedFiles = append(removedFiles, f)
							fmt.Printf("Removed stale file: %s\n", f)
						}
					}
				}
			}

			// Also clean untracked files
			cleanCmd := exec.CommandContext(ctx, "git", "-C", setup.gitDir, "clean", "-fd")
			var cleanOutput bytes.Buffer
			cleanCmd.Stdout = &cleanOutput
			if err := cleanCmd.Run(); err == nil {
				// Parse clean output to log removed files
				for _, line := range strings.Split(cleanOutput.String(), "\n") {
					if strings.HasPrefix(line, "Removing ") {
						f := strings.TrimPrefix(line, "Removing ")
						removedFiles = append(removedFiles, f)
						fmt.Printf("Removed untracked: %s\n", f)
					}
				}
			}
		}
	}

	// Get commit SHA
	commitCmd := exec.CommandContext(ctx, "git", "-C", setup.gitDir, "rev-parse", "HEAD")
	commitOut, err := commitCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get commit: %w", err)
	}

	return &GitCloneResult{
		Commit:       strings.TrimSpace(string(commitOut)),
		Branch:       setup.branch,
		RemovedFiles: removedFiles,
	}, nil
}
