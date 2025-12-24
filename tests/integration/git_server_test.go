package integration_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// GitServer represents an SSH Git server sidecar container for integration tests.
// It runs an Ubuntu container with OpenSSH server and git to provide a real Git
// repository that can be accessed via SSH.
type GitServer struct {
	t           testing.TB
	prefix      string
	containerID string
	ipAddress   string
	r           *Runner
	ctx         context.Context
}

// NewGitServer creates and starts a new Git server sidecar container.
// The server is configured with OpenSSH and git, ready to accept SSH connections.
func NewGitServer(t testing.TB, prefix string) *GitServer {
	t.Helper()

	r := NewRunner(t)
	ctx := context.Background()

	g := &GitServer{
		t:      t,
		prefix: prefix,
		r:      r,
		ctx:    ctx,
	}

	containerName := prefix + "-gitserver"

	// Clean up any existing container
	_, _ = r.Exec(ctx, ExecSpec{
		Cmd:    "docker",
		Args:   []string{"rm", "-f", containerName},
		Prefix: "[docker]",
	})

	// Start Ubuntu container
	res, err := r.Exec(ctx, ExecSpec{
		Cmd:    "docker",
		Args:   []string{"run", "-d", "--name", containerName, "ubuntu:22.04", "sleep", "infinity"},
		Prefix: "[docker]",
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("failed to start git server container: %v", err)
	}
	g.containerID = strings.TrimSpace(res.Output)

	// Register cleanup
	t.Cleanup(func() {
		g.Cleanup()
	})

	// Install required packages
	g.execInContainer("apt-get", "update", "-qq")
	g.execInContainer("apt-get", "install", "-y", "--no-install-recommends", "-qq", "openssh-server", "git")

	// Configure SSH
	g.execInContainer("mkdir", "-p", "/run/sshd")
	g.execInContainer("ssh-keygen", "-A")
	g.execInContainer("sh", "-c", "echo 'PermitRootLogin yes' >> /etc/ssh/sshd_config")
	g.execInContainer("sh", "-c", "echo 'PubkeyAuthentication yes' >> /etc/ssh/sshd_config")
	g.execInContainer("mkdir", "-p", "/root/.ssh")
	g.execInContainer("chmod", "700", "/root/.ssh")

	// Start SSH server
	g.execInContainer("/usr/sbin/sshd")

	// Create git directory
	g.execInContainer("mkdir", "-p", "/git")

	// Get container IP address
	g.ipAddress = g.getContainerIP()
	if g.ipAddress == "" {
		t.Fatal("failed to get git server container IP address")
	}

	// Wait for SSH to be ready
	g.waitForSSH()

	t.Logf("Git server started at %s", g.ipAddress)

	return g
}

// execInContainer executes a command inside the git server container.
func (g *GitServer) execInContainer(args ...string) string {
	g.t.Helper()

	dockerArgs := append([]string{"exec", g.containerID}, args...)
	res, err := g.r.Exec(g.ctx, ExecSpec{
		Cmd:    "docker",
		Args:   dockerArgs,
		Prefix: "[gitserver]",
	})
	if err != nil || res.ExitCode != 0 {
		g.t.Fatalf("exec in git server failed: %s %v", strings.Join(args, " "), err)
	}
	return res.Output
}

// getContainerIP retrieves the IP address of the git server container.
func (g *GitServer) getContainerIP() string {
	g.t.Helper()

	res, err := g.r.Exec(g.ctx, ExecSpec{
		Cmd:    "docker",
		Args:   []string{"inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", g.containerID},
		Prefix: "[docker]",
	})
	if err != nil || res.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(res.Output)
}

// waitForSSH waits for the SSH server to be ready to accept connections.
func (g *GitServer) waitForSSH() {
	g.t.Helper()

	// Wait for sshd to be listening
	for i := 0; i < 30; i++ {
		res, _ := g.r.Exec(g.ctx, ExecSpec{
			Cmd:    "docker",
			Args:   []string{"exec", g.containerID, "sh", "-c", "pgrep -x sshd > /dev/null && echo ready"},
			Prefix: "[gitserver]",
		})
		if strings.TrimSpace(res.Output) == "ready" {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	g.t.Fatal("timeout waiting for SSH server to be ready")
}

// GetSSHURL returns the SSH URL for accessing a repository on this server.
// Format: root@<ip>:/git/<repo>.git
func (g *GitServer) GetSSHURL(repoName string) string {
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
	g.execInContainer("git", "init", "--bare", repoPath)
	return nil
}

// AddAuthorizedKey adds a public key to the server's authorized_keys file.
func (g *GitServer) AddAuthorizedKey(pubKey string) error {
	g.t.Helper()

	// Escape single quotes in the key
	escapedKey := strings.ReplaceAll(pubKey, "'", "'\"'\"'")
	g.execInContainer("sh", "-c", fmt.Sprintf("echo '%s' >> /root/.ssh/authorized_keys", escapedKey))
	g.execInContainer("chmod", "600", "/root/.ssh/authorized_keys")
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
	g.execInContainer("mkdir", "-p", workRepoPath)
	g.execInContainer("git", "-C", workRepoPath, "init")
	g.execInContainer("git", "-C", workRepoPath, "config", "user.email", "test@test.local")
	g.execInContainer("git", "-C", workRepoPath, "config", "user.name", "Test")

	// Create files
	for filename, content := range files {
		// Handle subdirectories
		if strings.Contains(filename, "/") {
			dir := filename[:strings.LastIndex(filename, "/")]
			g.execInContainer("mkdir", "-p", workRepoPath+"/"+dir)
		}
		// Write file content using quoted heredoc (no escaping needed - content is literal)
		g.execInContainer("sh", "-c", fmt.Sprintf("cat > '%s/%s' << 'STEVEDORE_EOF'\n%s\nSTEVEDORE_EOF", workRepoPath, filename, content))
	}

	// Commit and push using local file protocol
	g.execInContainer("git", "-C", workRepoPath, "add", ".")
	g.execInContainer("git", "-C", workRepoPath, "commit", "-m", "Initial commit")
	g.execInContainer("git", "-C", workRepoPath, "branch", "-M", "main")
	g.execInContainer("git", "-C", workRepoPath, "remote", "add", "origin", bareRepoPath)
	g.execInContainer("git", "-C", workRepoPath, "push", "-u", "origin", "main")

	// Clean up working directory
	g.execInContainer("rm", "-rf", workRepoPath)

	return nil
}

// GetHostKeyFingerprint returns the SSH host key fingerprint of the server.
func (g *GitServer) GetHostKeyFingerprint() string {
	g.t.Helper()

	output := g.execInContainer("ssh-keygen", "-lf", "/etc/ssh/ssh_host_ed25519_key.pub")
	return strings.TrimSpace(output)
}

// Cleanup removes the git server container.
func (g *GitServer) Cleanup() {
	if g.containerID == "" {
		return
	}

	_, _ = g.r.Exec(g.ctx, ExecSpec{
		Cmd:    "docker",
		Args:   []string{"rm", "-f", g.containerID},
		Prefix: "[docker]",
	})
	g.containerID = ""
}

// TestGitServer_Basic tests that the GitServer helper works correctly.
func TestGitServer_Basic(t *testing.T) {
	prefix := fmt.Sprintf("stevedore-it-gitserver-%d", time.Now().UnixNano())
	gs := NewGitServer(t, prefix)

	// Create a test repo
	if err := gs.InitRepoWithContent("test-repo", map[string]string{
		"README.md": "# Test Repository\n",
		"docker-compose.yaml": `services:
  web:
    image: nginx:alpine
`,
	}); err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	// Verify the repo was created
	sshURL := gs.GetSSHURL("test-repo")
	if !strings.Contains(sshURL, "root@") {
		t.Errorf("expected SSH URL to contain root@, got: %s", sshURL)
	}
	if !strings.Contains(sshURL, "/git/test-repo.git") {
		t.Errorf("expected SSH URL to contain /git/test-repo.git, got: %s", sshURL)
	}

	t.Logf("Git server test passed. SSH URL: %s", sshURL)
}
