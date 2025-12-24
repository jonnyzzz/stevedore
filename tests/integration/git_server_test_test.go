package integration_test

import (
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
