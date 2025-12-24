package integration_test

import (
	"fmt"
	"strings"
	"testing"
)

// TestGitServer_Basic tests that the GitServer helper works correctly.
func TestGitServer_Basic(t *testing.T) {
	gs := NewGitServer(t)

	// Verify IP address is set
	if gs.GetIPAddress() == "" {
		t.Fatal("expected non-empty IP address")
	}

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

	// Verify the SSH URL format
	sshURL := gs.GetSshUrl("test-repo")
	if !strings.Contains(sshURL, "root@") {
		t.Errorf("expected SSH URL to contain root@, got: %s", sshURL)
	}
	if !strings.Contains(sshURL, "/git/test-repo.git") {
		t.Errorf("expected SSH URL to contain /git/test-repo.git, got: %s", sshURL)
	}

	t.Logf("Git server test passed. SSH URL: %s", sshURL)
}

// TestGitServer_MultipleRepos tests creating multiple repositories.
func TestGitServer_MultipleRepos(t *testing.T) {
	gs := NewGitServer(t)

	// Create first repo
	if err := gs.InitRepoWithContent("repo1", map[string]string{
		"file1.txt": "content1",
	}); err != nil {
		t.Fatalf("failed to init repo1: %v", err)
	}

	// Create second repo
	if err := gs.InitRepoWithContent("repo2", map[string]string{
		"file2.txt": "content2",
	}); err != nil {
		t.Fatalf("failed to init repo2: %v", err)
	}

	// Verify both URLs are different
	url1 := gs.GetSshUrl("repo1")
	url2 := gs.GetSshUrl("repo2")
	if url1 == url2 {
		t.Errorf("expected different URLs for different repos, got same: %s", url1)
	}
}

// TestGitServer_SubdirectoryFiles tests creating files in subdirectories.
func TestGitServer_SubdirectoryFiles(t *testing.T) {
	gs := NewGitServer(t)

	// Create repo with files in subdirectories
	if err := gs.InitRepoWithContent("nested-repo", map[string]string{
		"README.md":          "# Root file\n",
		"src/main.go":        "package main\n",
		"src/pkg/helper.go":  "package pkg\n",
		"docker/Dockerfile":  "FROM alpine\n",
		"config/app.yaml":    "key: value\n",
	}); err != nil {
		t.Fatalf("failed to init repo with subdirectories: %v", err)
	}

	t.Log("Successfully created repo with nested files")
}

// TestGitServer_SpecialCharacters tests files with special characters.
func TestGitServer_SpecialCharacters(t *testing.T) {
	gs := NewGitServer(t)

	// Create repo with special characters in content
	if err := gs.InitRepoWithContent("special-repo", map[string]string{
		"quotes.txt":   `This has "double" and 'single' quotes`,
		"newlines.txt": "line1\nline2\nline3",
		"yaml.yaml": `services:
  web:
    environment:
      - KEY='value with quotes'
      - OTHER="double quoted"
`,
	}); err != nil {
		t.Fatalf("failed to init repo with special characters: %v", err)
	}

	t.Log("Successfully created repo with special characters")
}

// TestGitServer_AuthorizedKey tests adding authorized keys.
func TestGitServer_AuthorizedKey(t *testing.T) {
	gs := NewGitServer(t)

	// Add a sample public key
	sampleKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyDataHere test@example.com"
	if err := gs.AddAuthorizedKey(sampleKey); err != nil {
		t.Fatalf("failed to add authorized key: %v", err)
	}

	t.Log("Successfully added authorized key")
}

// TestGitServer_HostKeyFingerprint tests getting the host key fingerprint.
func TestGitServer_HostKeyFingerprint(t *testing.T) {
	gs := NewGitServer(t)

	fingerprint := gs.GetHostKeyFingerprint()
	if fingerprint == "" {
		t.Fatal("expected non-empty host key fingerprint")
	}
	if !strings.Contains(fingerprint, "SHA256:") {
		t.Errorf("expected fingerprint to contain SHA256:, got: %s", fingerprint)
	}

	t.Logf("Host key fingerprint: %s", fingerprint)
}

// TestGitServer_SshClone tests cloning a repository via SSH using a client container.
// This test uses an alpine/git container as the git client since Docker Desktop
// on macOS doesn't allow direct host-to-container network access.
func TestGitServer_SshClone(t *testing.T) {
	gs := NewGitServer(t)

	// Create a repo with content
	if err := gs.InitRepoWithContent("clone-test", map[string]string{
		"README.md": "# Clone Test\n",
		"file.txt":  "hello world\n",
	}); err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	// Create a client container that can access the git server
	client := NewTestContainerWithOptions(t, ContainerOptions{
		Dockerfile: "Dockerfile.gitclient",
	})

	// Generate SSH key pair in the client
	client.ExecOK("ssh-keygen", "-t", "ed25519", "-f", "/root/.ssh/id_ed25519", "-N", "", "-q")

	// Get the public key and add to git server
	pubKey := client.ExecOK("cat", "/root/.ssh/id_ed25519.pub")
	if err := gs.AddAuthorizedKey(pubKey); err != nil {
		t.Fatalf("failed to add authorized key: %v", err)
	}

	// Clone the repo via SSH
	sshURL := gs.GetSshUrl("clone-test")
	client.ExecOK("sh", "-c", fmt.Sprintf(
		"GIT_SSH_COMMAND='ssh -o StrictHostKeyChecking=no' git clone %s /tmp/clone",
		sshURL,
	))

	// Verify the files were cloned
	readmeContent := strings.TrimSpace(client.ExecOK("cat", "/tmp/clone/README.md"))
	if readmeContent != "# Clone Test" {
		t.Errorf("unexpected README content: %q", readmeContent)
	}

	fileContent := strings.TrimSpace(client.ExecOK("cat", "/tmp/clone/file.txt"))
	if fileContent != "hello world" {
		t.Errorf("unexpected file.txt content: %q", fileContent)
	}

	t.Log("Successfully cloned repository via SSH")
}

// TestGitServer_SshPush tests pushing changes to the repository via SSH.
func TestGitServer_SshPush(t *testing.T) {
	gs := NewGitServer(t)

	// Create a repo with content
	if err := gs.InitRepoWithContent("push-test", map[string]string{
		"README.md": "# Push Test\n",
	}); err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	// Create client container
	client := NewTestContainerWithOptions(t, ContainerOptions{
		Dockerfile: "Dockerfile.gitclient",
	})

	// Generate SSH key and add to server
	client.ExecOK("ssh-keygen", "-t", "ed25519", "-f", "/root/.ssh/id_ed25519", "-N", "", "-q")
	pubKey := client.ExecOK("cat", "/root/.ssh/id_ed25519.pub")
	if err := gs.AddAuthorizedKey(pubKey); err != nil {
		t.Fatalf("failed to add authorized key: %v", err)
	}

	sshURL := gs.GetSshUrl("push-test")

	// Clone the repo
	client.ExecOK("sh", "-c", fmt.Sprintf(
		"GIT_SSH_COMMAND='ssh -o StrictHostKeyChecking=no' git clone %s /tmp/repo",
		sshURL,
	))

	// Configure git
	client.ExecOK("git", "-C", "/tmp/repo", "config", "user.email", "test@example.com")
	client.ExecOK("git", "-C", "/tmp/repo", "config", "user.name", "Test User")

	// Create a new file, commit and push
	client.ExecOK("sh", "-c", "echo 'new content' > /tmp/repo/new-file.txt")
	client.ExecOK("git", "-C", "/tmp/repo", "add", "new-file.txt")
	client.ExecOK("git", "-C", "/tmp/repo", "commit", "-m", "Add new file")
	client.ExecOK("sh", "-c", fmt.Sprintf(
		"GIT_SSH_COMMAND='ssh -o StrictHostKeyChecking=no' git -C /tmp/repo push",
	))

	// Verify by cloning to a new directory
	client.ExecOK("sh", "-c", fmt.Sprintf(
		"GIT_SSH_COMMAND='ssh -o StrictHostKeyChecking=no' git clone %s /tmp/verify",
		sshURL,
	))

	verifyContent := strings.TrimSpace(client.ExecOK("cat", "/tmp/verify/new-file.txt"))
	if verifyContent != "new content" {
		t.Errorf("unexpected content in pushed file: %q", verifyContent)
	}

	t.Log("Successfully pushed changes via SSH")
}

// TestGitServer_SshPull tests pulling changes from the repository via SSH.
func TestGitServer_SshPull(t *testing.T) {
	gs := NewGitServer(t)

	// Create a repo with content
	if err := gs.InitRepoWithContent("pull-test", map[string]string{
		"README.md": "# Pull Test\n",
	}); err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	// Create client container (simulates two developers using same container with two clones)
	client := NewTestContainerWithOptions(t, ContainerOptions{
		Dockerfile: "Dockerfile.gitclient",
	})

	// Generate SSH key and add to server
	client.ExecOK("ssh-keygen", "-t", "ed25519", "-f", "/root/.ssh/id_ed25519", "-N", "", "-q")
	pubKey := client.ExecOK("cat", "/root/.ssh/id_ed25519.pub")
	if err := gs.AddAuthorizedKey(pubKey); err != nil {
		t.Fatalf("failed to add authorized key: %v", err)
	}

	sshURL := gs.GetSshUrl("pull-test")

	// Clone to two directories (simulating two developers)
	client.ExecOK("sh", "-c", fmt.Sprintf(
		"GIT_SSH_COMMAND='ssh -o StrictHostKeyChecking=no' git clone %s /tmp/clone1",
		sshURL,
	))
	client.ExecOK("sh", "-c", fmt.Sprintf(
		"GIT_SSH_COMMAND='ssh -o StrictHostKeyChecking=no' git clone %s /tmp/clone2",
		sshURL,
	))

	// Make a change in clone1 and push
	client.ExecOK("git", "-C", "/tmp/clone1", "config", "user.email", "test@example.com")
	client.ExecOK("git", "-C", "/tmp/clone1", "config", "user.name", "Test")
	client.ExecOK("sh", "-c", "echo 'from clone1' > /tmp/clone1/from-clone1.txt")
	client.ExecOK("git", "-C", "/tmp/clone1", "add", "from-clone1.txt")
	client.ExecOK("git", "-C", "/tmp/clone1", "commit", "-m", "Add file from clone1")
	client.ExecOK("sh", "-c", "GIT_SSH_COMMAND='ssh -o StrictHostKeyChecking=no' git -C /tmp/clone1 push")

	// Pull in clone2
	client.ExecOK("sh", "-c", "GIT_SSH_COMMAND='ssh -o StrictHostKeyChecking=no' git -C /tmp/clone2 pull")

	// Verify the file from clone1 is now in clone2
	content := strings.TrimSpace(client.ExecOK("cat", "/tmp/clone2/from-clone1.txt"))
	if content != "from clone1" {
		t.Errorf("unexpected content after pull: %q", content)
	}

	t.Log("Successfully pulled changes via SSH")
}

// TestGitServer_SshBranches tests working with branches via SSH.
func TestGitServer_SshBranches(t *testing.T) {
	gs := NewGitServer(t)

	// Create a repo
	if err := gs.InitRepoWithContent("branch-test", map[string]string{
		"README.md": "# Branch Test\n",
	}); err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	// Create client container
	client := NewTestContainerWithOptions(t, ContainerOptions{
		Dockerfile: "Dockerfile.gitclient",
	})

	// Generate SSH key and add to server
	client.ExecOK("ssh-keygen", "-t", "ed25519", "-f", "/root/.ssh/id_ed25519", "-N", "", "-q")
	pubKey := client.ExecOK("cat", "/root/.ssh/id_ed25519.pub")
	if err := gs.AddAuthorizedKey(pubKey); err != nil {
		t.Fatalf("failed to add authorized key: %v", err)
	}

	sshURL := gs.GetSshUrl("branch-test")

	// Clone
	client.ExecOK("sh", "-c", fmt.Sprintf(
		"GIT_SSH_COMMAND='ssh -o StrictHostKeyChecking=no' git clone %s /tmp/repo",
		sshURL,
	))

	// Configure git
	client.ExecOK("git", "-C", "/tmp/repo", "config", "user.email", "test@example.com")
	client.ExecOK("git", "-C", "/tmp/repo", "config", "user.name", "Test")

	// Create and switch to a new branch
	client.ExecOK("git", "-C", "/tmp/repo", "checkout", "-b", "feature-branch")

	// Add a file on the branch
	client.ExecOK("sh", "-c", "echo 'feature' > /tmp/repo/feature.txt")
	client.ExecOK("git", "-C", "/tmp/repo", "add", "feature.txt")
	client.ExecOK("git", "-C", "/tmp/repo", "commit", "-m", "Add feature")

	// Push the new branch
	client.ExecOK("sh", "-c", "GIT_SSH_COMMAND='ssh -o StrictHostKeyChecking=no' git -C /tmp/repo push -u origin feature-branch")

	// Verify branch exists by cloning with branch
	client.ExecOK("sh", "-c", fmt.Sprintf(
		"GIT_SSH_COMMAND='ssh -o StrictHostKeyChecking=no' git clone -b feature-branch %s /tmp/verify",
		sshURL,
	))

	// Verify the feature file exists
	content := strings.TrimSpace(client.ExecOK("cat", "/tmp/verify/feature.txt"))
	if content != "feature" {
		t.Errorf("unexpected content: %q", content)
	}

	t.Log("Successfully worked with branches via SSH")
}
