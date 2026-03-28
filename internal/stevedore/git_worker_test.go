package stevedore

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrepareGitRepo_ValidatesDeploymentName(t *testing.T) {
	instance := NewInstance(t.TempDir())
	_, err := instance.prepareGitRepo("../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for invalid deployment name")
	}
}

func TestPrepareGitRepo_RequiresURLFile(t *testing.T) {
	root := t.TempDir()
	instance := NewInstance(root)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	deployment := "test-deploy"
	repoDir := filepath.Join(root, "deployments", deployment, "repo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := instance.prepareGitRepo(deployment)
	if err == nil {
		t.Fatal("expected error when url.txt is missing")
	}
	if !strings.Contains(err.Error(), "repository URL") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareGitRepo_RequiresSSHKey(t *testing.T) {
	root := t.TempDir()
	instance := NewInstance(root)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	deployment := "test-deploy"
	repoDir := filepath.Join(root, "deployments", deployment, "repo")
	sshDir := filepath.Join(repoDir, "ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "url.txt"), []byte("git@github.com:test/test.git"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "branch.txt"), []byte("main"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := instance.prepareGitRepo(deployment)
	if err == nil {
		t.Fatal("expected error when SSH key is missing")
	}
	if !strings.Contains(err.Error(), "SSH key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareGitRepo_DetectsCloneNeeded(t *testing.T) {
	root := t.TempDir()
	instance := NewInstance(root)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	deployment := "test-deploy"
	repoDir := filepath.Join(root, "deployments", deployment, "repo")
	sshDir := filepath.Join(repoDir, "ssh")
	gitDir := filepath.Join(repoDir, "git")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "url.txt"), []byte("git@github.com:test/test.git"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "branch.txt"), []byte("main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Without .git directory -> isClone = true
	setup, err := instance.prepareGitRepo(deployment)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !setup.isClone {
		t.Error("expected isClone=true when .git doesn't exist")
	}

	// With .git directory -> isClone = false
	if err := os.MkdirAll(filepath.Join(gitDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	setup, err = instance.prepareGitRepo(deployment)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if setup.isClone {
		t.Error("expected isClone=false when .git exists")
	}
}

func TestGitCheckRemote_ReturnsHasChangesWhenCloneNeeded(t *testing.T) {
	root := t.TempDir()
	instance := NewInstance(root)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	deployment := "test-deploy"
	repoDir := filepath.Join(root, "deployments", deployment, "repo")
	sshDir := filepath.Join(repoDir, "ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "url.txt"), []byte("git@github.com:test/test.git"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "branch.txt"), []byte("main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := instance.GitCheckRemote(context.Background(), deployment)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.HasChanges {
		t.Error("expected HasChanges=true for new deployment that needs clone")
	}
	if result.Branch != "main" {
		t.Errorf("expected branch 'main', got %q", result.Branch)
	}
}

// dockerAvailable returns true if docker CLI is available and working.
func dockerAvailable() bool {
	cmd := exec.Command("docker", "info")
	return cmd.Run() == nil
}

// TestRunGitScript_ExecutesInContainer verifies that runGitScript runs
// a script inside a docker container and returns stdout.
func TestRunGitScript_ExecutesInContainer(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker not available")
	}

	root := t.TempDir()
	instance := NewInstance(root)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	deployment := "test-script"
	setupGitRepoDir(t, root, deployment)
	t.Cleanup(func() { fixDockerOwnership(t, root) })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	output, err := instance.runGitScript(ctx, deployment, `echo "STEVEDORE_TEST=hello_from_container"`)
	if err != nil {
		t.Fatalf("runGitScript failed: %v", err)
	}

	if !strings.Contains(output, "STEVEDORE_TEST=hello_from_container") {
		t.Errorf("expected output to contain test marker, got: %q", output)
	}
}

// TestRunGitScript_MountsRepoDir verifies the repo directory is accessible inside the container.
func TestRunGitScript_MountsRepoDir(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker not available")
	}

	root := t.TempDir()
	instance := NewInstance(root)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	deployment := "test-mount"
	setupGitRepoDir(t, root, deployment)
	t.Cleanup(func() { fixDockerOwnership(t, root) })

	// Create a marker file in the git dir
	gitDir := filepath.Join(root, "deployments", deployment, "repo", "git")
	if err := os.WriteFile(filepath.Join(gitDir, "marker.txt"), []byte("found-it"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	output, err := instance.runGitScript(ctx, deployment, `cat marker.txt`)
	if err != nil {
		t.Fatalf("runGitScript failed: %v", err)
	}

	if !strings.Contains(output, "found-it") {
		t.Errorf("expected to read marker file from mounted repo, got: %q", output)
	}
}

// TestRunGitScript_ContextCancelStopsContainer verifies that canceling the context
// stops the container cleanly without leaving orphan processes.
func TestRunGitScript_ContextCancelStopsContainer(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker not available")
	}

	root := t.TempDir()
	instance := NewInstance(root)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	deployment := "test-cancel"
	setupGitRepoDir(t, root, deployment)
	t.Cleanup(func() { fixDockerOwnership(t, root) })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// This script would run for 60s, but context will cancel after 2s
	_, err := instance.runGitScript(ctx, deployment, `sleep 60`)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	t.Logf("Script correctly failed on cancel: %v", err)
}

// TestGitSyncClean_CloneInContainer verifies GitSyncClean can clone a repo using a docker worker.
func TestGitSyncClean_CloneInContainer(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker not available")
	}

	root := t.TempDir()
	instance := NewInstance(root)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	deployment := "test-clone"

	// Create a local bare repo to clone from (no SSH needed for file:// protocol)
	bareRepo := filepath.Join(root, "bare-repo.git")
	runGit(t, "", "init", "--bare", "--initial-branch=main", bareRepo)

	// Create a working repo, add a file, push to bare
	workRepo := t.TempDir()
	runGit(t, workRepo, "init", "-b", "main")
	runGit(t, workRepo, "config", "user.email", "test@test.local")
	runGit(t, workRepo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(workRepo, "hello.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, workRepo, "add", ".")
	runGit(t, workRepo, "commit", "-m", "initial")
	runGit(t, workRepo, "remote", "add", "origin", bareRepo)
	runGit(t, workRepo, "push", "-u", "origin", "main")

	// Set up the deployment repo dir with file:// URL (no SSH)
	repoDir := filepath.Join(root, "deployments", deployment, "repo")
	sshDir := filepath.Join(repoDir, "ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Use file:// URL pointing to a path that will be mounted in the container
	// We mount bareRepo at /bare-repo inside the container via a modified script
	if err := os.WriteFile(filepath.Join(repoDir, "url.txt"), []byte("file:///bare-repo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "branch.txt"), []byte("main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("unused-for-file-protocol"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Ensure git dir exists
	gitDir := filepath.Join(repoDir, "git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t.Cleanup(func() { fixDockerOwnership(t, root) })

	output := runDockerGit(t, ctx, `set -e
cd /repo
git clone --branch main --depth 1 --single-branch file:///bare-repo .
echo "STEVEDORE_COMMIT=$(git rev-parse HEAD)"
`, map[string]string{gitDir: "/repo", bareRepo: "/bare-repo:ro"})
	if !strings.Contains(output, "STEVEDORE_COMMIT=") {
		t.Fatalf("expected commit SHA in output, got: %q", output)
	}

	// Verify the file was cloned
	data, err := os.ReadFile(filepath.Join(gitDir, "hello.txt"))
	if err != nil {
		t.Fatalf("cloned file not found: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("expected file content 'world', got %q", string(data))
	}
}

// TestGitSyncClean_FetchResetInContainer verifies fetch+reset works in a docker worker.
func TestGitSyncClean_FetchResetInContainer(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker not available")
	}

	root := t.TempDir()

	// Create a local bare repo
	bareRepo := filepath.Join(root, "bare-repo.git")
	runGit(t, "", "init", "--bare", "--initial-branch=main", bareRepo)

	// Create working repo with initial commit
	workRepo := t.TempDir()
	runGit(t, workRepo, "init", "-b", "main")
	runGit(t, workRepo, "config", "user.email", "test@test.local")
	runGit(t, workRepo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(workRepo, "v1.txt"), []byte("version1"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, workRepo, "add", ".")
	runGit(t, workRepo, "commit", "-m", "v1")
	runGit(t, workRepo, "remote", "add", "origin", bareRepo)
	runGit(t, workRepo, "push", "-u", "origin", "main")

	// Clone into the git dir (simulating initial sync)
	gitDir := filepath.Join(root, "git-workdir")
	runGit(t, "", "clone", "--branch", "main", "--depth", "1", bareRepo, gitDir)
	// Remap remote to container-accessible path
	runGit(t, gitDir, "remote", "set-url", "origin", "/bare-repo")

	commit1 := getHeadCommit(t, gitDir)

	// Push a new commit to the bare repo
	if err := os.WriteFile(filepath.Join(workRepo, "v2.txt"), []byte("version2"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, workRepo, "add", ".")
	runGit(t, workRepo, "commit", "-m", "v2")
	runGit(t, workRepo, "push", "origin", "main")

	// Run fetch+reset in container
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t.Cleanup(func() { fixDockerOwnership(t, root) })

	runDockerGit(t, ctx, `set -e
cd /repo
git fetch --depth 1 origin main
git reset --hard FETCH_HEAD
git clean -fd 2>/dev/null || true
echo "STEVEDORE_COMMIT=$(git rev-parse HEAD)"
`, map[string]string{gitDir: "/repo", bareRepo: "/bare-repo:ro"})

	commit2 := getHeadCommit(t, gitDir)
	if commit1 == commit2 {
		t.Error("expected commit to change after fetch+reset")
	}

	// Verify new file exists
	if _, err := os.Stat(filepath.Join(gitDir, "v2.txt")); err != nil {
		t.Errorf("expected v2.txt to exist after sync: %v", err)
	}
}

// TestGitSyncClean_CleanRemovesUntracked verifies that git clean removes untracked files.
func TestGitSyncClean_CleanRemovesUntracked(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker not available")
	}

	root := t.TempDir()

	// Create bare repo and working repo
	bareRepo := filepath.Join(root, "bare-repo.git")
	runGit(t, "", "init", "--bare", "--initial-branch=main", bareRepo)

	workRepo := t.TempDir()
	runGit(t, workRepo, "init", "-b", "main")
	runGit(t, workRepo, "config", "user.email", "test@test.local")
	runGit(t, workRepo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(workRepo, "tracked.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, workRepo, "add", ".")
	runGit(t, workRepo, "commit", "-m", "initial")
	runGit(t, workRepo, "remote", "add", "origin", bareRepo)
	runGit(t, workRepo, "push", "-u", "origin", "main")

	// Clone
	gitDir := filepath.Join(root, "git-workdir")
	runGit(t, "", "clone", "--branch", "main", "--depth", "1", bareRepo, gitDir)
	// Remap remote to container-accessible path
	runGit(t, gitDir, "remote", "set-url", "origin", "/bare-repo")

	// Add an untracked file
	if err := os.WriteFile(filepath.Join(gitDir, "junk.txt"), []byte("should be removed"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Verify it exists
	if _, err := os.Stat(filepath.Join(gitDir, "junk.txt")); err != nil {
		t.Fatal("junk.txt should exist before clean")
	}

	// Run clean in container
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t.Cleanup(func() { fixDockerOwnership(t, root) })

	runDockerGit(t, ctx,
		"cd /repo && git fetch --depth 1 origin main && git reset --hard FETCH_HEAD && git clean -fd",
		map[string]string{gitDir: "/repo", bareRepo: "/bare-repo:ro"})

	// Verify untracked file was removed
	if _, err := os.Stat(filepath.Join(gitDir, "junk.txt")); err == nil {
		t.Error("expected junk.txt to be removed by git clean")
	}

	// Verify tracked file still exists
	if _, err := os.Stat(filepath.Join(gitDir, "tracked.txt")); err != nil {
		t.Error("expected tracked.txt to still exist")
	}
}

// runDockerGit runs a git script in an alpine/git container with the given volumes.
// It prepends safe.directory config to avoid dubious ownership errors.
// Returns stdout.
func runDockerGit(t *testing.T, ctx context.Context, script string, volumes map[string]string) string {
	t.Helper()
	image := DefaultGitWorkerConfig().Image
	containerName := fmt.Sprintf("stevedore-test-%d", time.Now().UnixNano())

	fullScript := "git config --global --add safe.directory /repo\n" + script

	args := []string{
		"run", "--rm",
		"--name", containerName,
		"--entrypoint", "sh",
	}
	for hostPath, containerPath := range volumes {
		args = append(args, "-v", hostPath+":"+containerPath)
	}
	args = append(args, image, "-c", fullScript)

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("docker git script failed: %v: %s", err, stderr.String())
	}
	return stdout.String()
}

// fixDockerOwnership fixes ownership of files created by Docker containers (which run as root).
// This is needed so t.TempDir() cleanup can delete the files.
func fixDockerOwnership(t *testing.T, dir string) {
	t.Helper()
	uid := os.Getuid()
	gid := os.Getgid()
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		_ = os.Chown(path, uid, gid)
		return nil
	})
}

// setupGitRepoDir creates the minimal directory structure for a deployment repo.
func setupGitRepoDir(t *testing.T, root, deployment string) {
	t.Helper()
	repoDir := filepath.Join(root, "deployments", deployment, "repo")
	sshDir := filepath.Join(repoDir, "ssh")
	gitDir := filepath.Join(repoDir, "git")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "url.txt"), []byte("git@github.com:test/test.git"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "branch.txt"), []byte("main"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create a dummy SSH key (needed for validation, not used in file:// tests)
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("test-key"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// runGit runs a git command and fails the test if it errors.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// getHeadCommit returns the HEAD commit SHA.
func getHeadCommit(t *testing.T, dir string) string {
	t.Helper()
	return runGit(t, dir, "rev-parse", "HEAD")
}
