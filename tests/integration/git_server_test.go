package integration_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// GitServer represents an SSH Git server sidecar container for integration tests.
// It runs a container with OpenSSH server and git to provide a real Git
// repository that can be accessed via SSH.
// Uses Dockerfile.gitserver from testdata directory.
type GitServer struct {
	t         testing.TB
	container *TestContainer
	ipAddress string
}

// NewGitServer creates and starts a new Git server sidecar container.
// The server is built from Dockerfile.gitserver and configured with OpenSSH and git.
func NewGitServer(t testing.TB) *GitServer {
	t.Helper()

	container := NewTestContainerWithOptions(t, ContainerOptions{
		Dockerfile:             "Dockerfile.gitserver",
		MountDockerSocket:      false,
		MountStevedoreRepoRoot: false,
	})

	g := &GitServer{
		t:         t,
		container: container,
	}

	// Get container IP address
	g.ipAddress = container.GetIP()
	if g.ipAddress == "" {
		t.Fatal("failed to get git server container IP address")
	}

	// Wait for SSH to be ready
	g.waitForSSH()

	t.Logf("Git server started at %s", g.ipAddress)

	return g
}

// waitForSSH waits for the SSH server to be ready to accept connections.
func (g *GitServer) waitForSSH() {
	g.t.Helper()

	for i := 0; i < 30; i++ {
		res, _ := g.container.Exec("sh", "-c", "pgrep -x sshd > /dev/null && echo ready")
		if strings.TrimSpace(res.Output) == "ready" {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	g.t.Fatal("timeout waiting for SSH server to be ready")
}

// GetSshUrl returns the SSH URL for accessing a repository on this server.
// Format: root@<ip>:/git/<repo>.git
func (g *GitServer) GetSshUrl(repoName string) string {
	return fmt.Sprintf("root@%s:/git/%s.git", g.ipAddress, repoName)
}

// GetIPAddress returns the IP address of the git server container.
func (g *GitServer) GetIPAddress() string {
	return g.ipAddress
}

// CreateBareRepo creates a bare git repository on the server.
func (g *GitServer) CreateBareRepo(name string) error {
	g.t.Helper()

	repoPath := fmt.Sprintf("/git/%s.git", name)
	g.container.ExecOK("git", "init", "--bare", repoPath)
	return nil
}

// AddAuthorizedKey adds a public key to the server's authorized_keys file.
func (g *GitServer) AddAuthorizedKey(pubKey string) error {
	g.t.Helper()

	// Escape single quotes in the key
	escapedKey := strings.ReplaceAll(pubKey, "'", "'\"'\"'")
	g.container.ExecOK("sh", "-c", fmt.Sprintf("echo '%s' >> /root/.ssh/authorized_keys", escapedKey))
	g.container.ExecOK("chmod", "600", "/root/.ssh/authorized_keys")
	return nil
}

// InitRepoWithContent initializes a repository with the given files.
// The files map contains filename -> content pairs.
// This method creates a normal repo, commits, and pushes to the bare repo
// using local file protocol (no SSH needed for seeding).
func (g *GitServer) InitRepoWithContent(name string, files map[string]string) error {
	g.t.Helper()

	// Create bare repo first
	if err := g.CreateBareRepo(name); err != nil {
		return err
	}

	bareRepoPath := fmt.Sprintf("/git/%s.git", name)
	workRepoPath := fmt.Sprintf("/tmp/%s-work", name)

	// Create a working directory and initialize git
	g.container.ExecOK("mkdir", "-p", workRepoPath)
	g.container.ExecOK("git", "-C", workRepoPath, "init")
	g.container.ExecOK("git", "-C", workRepoPath, "config", "user.email", "test@test.local")
	g.container.ExecOK("git", "-C", workRepoPath, "config", "user.name", "Test")

	// Create files
	for filename, content := range files {
		// Handle subdirectories
		if strings.Contains(filename, "/") {
			dir := filename[:strings.LastIndex(filename, "/")]
			g.container.ExecOK("mkdir", "-p", workRepoPath+"/"+dir)
		}
		// Write file content using quoted heredoc (no escaping needed - content is literal)
		g.container.ExecOK("sh", "-c", fmt.Sprintf("cat > '%s/%s' << 'STEVEDORE_EOF'\n%s\nSTEVEDORE_EOF", workRepoPath, filename, content))
	}

	// Commit and push using local file protocol
	g.container.ExecOK("git", "-C", workRepoPath, "add", ".")
	g.container.ExecOK("git", "-C", workRepoPath, "commit", "-m", "Initial commit")
	g.container.ExecOK("git", "-C", workRepoPath, "branch", "-M", "main")
	g.container.ExecOK("git", "-C", workRepoPath, "remote", "add", "origin", bareRepoPath)
	g.container.ExecOK("git", "-C", workRepoPath, "push", "-u", "origin", "main")

	// Update bare repo HEAD to point to main (default is master)
	g.container.ExecOK("git", "-C", bareRepoPath, "symbolic-ref", "HEAD", "refs/heads/main")

	// Clean up working directory
	g.container.ExecOK("rm", "-rf", workRepoPath)

	return nil
}

// GetHostKeyFingerprint returns the SSH host key fingerprint of the server.
func (g *GitServer) GetHostKeyFingerprint() string {
	g.t.Helper()

	output := g.container.ExecOK("ssh-keygen", "-lf", "/etc/ssh/ssh_host_ed25519_key.pub")
	return strings.TrimSpace(output)
}

// UpdateFile updates or creates a file in the repository and commits/pushes the change.
func (g *GitServer) UpdateFile(repoName, filename, content string) error {
	g.t.Helper()

	bareRepoPath := fmt.Sprintf("/git/%s.git", repoName)
	workRepoPath := fmt.Sprintf("/tmp/%s-update", repoName)

	// Clone the repo locally
	g.container.ExecOK("git", "clone", bareRepoPath, workRepoPath)
	g.container.ExecOK("git", "-C", workRepoPath, "config", "user.email", "test@test.local")
	g.container.ExecOK("git", "-C", workRepoPath, "config", "user.name", "Test")

	// Handle subdirectories
	if strings.Contains(filename, "/") {
		dir := filename[:strings.LastIndex(filename, "/")]
		g.container.ExecOK("mkdir", "-p", workRepoPath+"/"+dir)
	}

	// Write file content
	g.container.ExecOK("sh", "-c", fmt.Sprintf("cat > '%s/%s' << 'STEVEDORE_EOF'\n%s\nSTEVEDORE_EOF", workRepoPath, filename, content))

	// Commit and push
	g.container.ExecOK("git", "-C", workRepoPath, "add", filename)
	g.container.ExecOK("git", "-C", workRepoPath, "commit", "-m", fmt.Sprintf("Update %s", filename))
	g.container.ExecOK("git", "-C", workRepoPath, "push", "origin", "main")

	// Clean up
	g.container.ExecOK("rm", "-rf", workRepoPath)

	return nil
}

// DeleteFile removes a file from the repository and commits/pushes the change.
func (g *GitServer) DeleteFile(repoName, filename string) error {
	g.t.Helper()

	bareRepoPath := fmt.Sprintf("/git/%s.git", repoName)
	workRepoPath := fmt.Sprintf("/tmp/%s-delete", repoName)

	// Clone the repo locally
	g.container.ExecOK("git", "clone", bareRepoPath, workRepoPath)
	g.container.ExecOK("git", "-C", workRepoPath, "config", "user.email", "test@test.local")
	g.container.ExecOK("git", "-C", workRepoPath, "config", "user.name", "Test")

	// Remove file
	g.container.ExecOK("git", "-C", workRepoPath, "rm", filename)

	// Commit and push
	g.container.ExecOK("git", "-C", workRepoPath, "commit", "-m", fmt.Sprintf("Delete %s", filename))
	g.container.ExecOK("git", "-C", workRepoPath, "push", "origin", "main")

	// Clean up
	g.container.ExecOK("rm", "-rf", workRepoPath)

	return nil
}

// InitRepoFromContainer initializes a repository with files from another container.
// This is useful for testing self-bootstrap scenarios where we push the current
// Stevedore source code to the git server.
// srcContainer: the container to copy files from
// srcPath: the path in the source container (e.g., "/tmp/stevedore-src")
// repoName: the name of the repository to create
func (g *GitServer) InitRepoFromContainer(srcContainer *TestContainer, srcPath, repoName string) error {
	g.t.Helper()

	// Create bare repo
	if err := g.CreateBareRepo(repoName); err != nil {
		return err
	}

	bareRepoPath := fmt.Sprintf("/git/%s.git", repoName)
	workRepoPath := fmt.Sprintf("/tmp/%s-init", repoName)

	// Create working directory in git server
	g.container.ExecOK("mkdir", "-p", workRepoPath)

	// Transfer files from source container to git server using tar pipe through host
	// Exclude .git directory to avoid conflicts with the new git repo we'll create
	// Step 1: Create tarball from source container (excluding .git)
	tarCmd := fmt.Sprintf("docker exec %s tar -C %s --exclude=.git --exclude=.tmp -cf - .", srcContainer.GetContainerName(), srcPath)

	// Step 2: Extract tarball into git server
	extractCmd := fmt.Sprintf("docker exec -i %s tar -C %s -xf -", g.container.GetContainerName(), workRepoPath)

	// Run the pipe: tar from source | extract to destination
	pipeCmd := fmt.Sprintf("%s | %s", tarCmd, extractCmd)

	// Execute on host (the test runner has access to docker)
	g.t.Logf("Transferring files from %s:%s to git server repo %s", srcContainer.GetContainerName(), srcPath, repoName)
	if _, err := srcContainer.r.Exec(srcContainer.ctx, ExecSpec{
		Cmd:    "sh",
		Args:   []string{"-c", pipeCmd},
		Prefix: "[tar-pipe]",
	}); err != nil {
		return fmt.Errorf("failed to transfer files: %w", err)
	}

	// Initialize git repo and commit
	g.container.ExecOK("git", "-C", workRepoPath, "init")
	g.container.ExecOK("git", "-C", workRepoPath, "config", "user.email", "test@test.local")
	g.container.ExecOK("git", "-C", workRepoPath, "config", "user.name", "Test")
	g.container.ExecOK("git", "-C", workRepoPath, "add", ".")
	g.container.ExecOK("git", "-C", workRepoPath, "commit", "-m", "Initial commit from source container")
	g.container.ExecOK("git", "-C", workRepoPath, "branch", "-M", "main")
	g.container.ExecOK("git", "-C", workRepoPath, "remote", "add", "origin", bareRepoPath)
	g.container.ExecOK("git", "-C", workRepoPath, "push", "-u", "origin", "main")

	// Update bare repo HEAD to point to main
	g.container.ExecOK("git", "-C", bareRepoPath, "symbolic-ref", "HEAD", "refs/heads/main")

	// Clean up working directory
	g.container.ExecOK("rm", "-rf", workRepoPath)

	g.t.Logf("Git repository %s initialized from container %s", repoName, srcContainer.GetContainerName())
	return nil
}
