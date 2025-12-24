package integration_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// GitServer represents an SSH Git server sidecar container for integration tests.
// It runs a container with OpenSSH server and git to provide a real Git
// repository that can be accessed via SSH.
// Uses Dockerfile.gitserver from testdata directory.
type GitServer struct {
	t           testing.TB
	prefix      string
	containerID string
	imageTag    string
	ipAddress   string
	docker      *dockerCLI
	ctx         context.Context
}

// NewGitServer creates and starts a new Git server sidecar container.
// The server is built from Dockerfile.gitserver and configured with OpenSSH and git.
func NewGitServer(t testing.TB, prefix string) *GitServer {
	t.Helper()

	ctx := context.Background()
	r := NewRunner(t)
	docker := &dockerCLI{t: t, ctx: ctx, r: r}

	containerName := prefix + "-gitserver"
	imageTag := "stevedore-gitserver:" + fmt.Sprintf("%d", time.Now().UnixNano())

	g := &GitServer{
		t:        t,
		prefix:   prefix,
		docker:   docker,
		ctx:      ctx,
		imageTag: imageTag,
	}

	// Clean up any existing container with same name
	docker.stopAndRemoveContainer(containerName)

	// Build the git server image
	repoRoot := testRepoRoot(t)
	dockerfilePath := filepath.Join(repoRoot, "tests", "integration", "testdata", "Dockerfile.gitserver")
	docker.runOK("build", "-t", imageTag, "-f", dockerfilePath, filepath.Dir(dockerfilePath))

	// Start the container
	output := docker.runOK("run", "-d", "--name", containerName, imageTag)
	g.containerID = strings.TrimSpace(output)

	// Register cleanup
	t.Cleanup(func() {
		g.Cleanup()
	})

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
	res, err := g.docker.r.Exec(g.ctx, ExecSpec{
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

	output := g.docker.runOK("inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", g.containerID)
	return strings.TrimSpace(output)
}

// waitForSSH waits for the SSH server to be ready to accept connections.
func (g *GitServer) waitForSSH() {
	g.t.Helper()

	for i := 0; i < 30; i++ {
		res, _ := g.docker.r.Exec(g.ctx, ExecSpec{
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

// Cleanup removes the git server container and image.
func (g *GitServer) Cleanup() {
	if g.containerID != "" {
		g.docker.stopAndRemoveContainer(g.containerID)
		g.containerID = ""
	}
	if g.imageTag != "" {
		g.docker.removeImage(g.imageTag)
		g.imageTag = ""
	}
}
