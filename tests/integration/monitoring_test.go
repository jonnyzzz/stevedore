package integration_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMonitoringWorkflow tests the git check and sync-clean functionality:
// 1. Set up git server with initial repo
// 2. Install stevedore and add deployment
// 3. Sync the repository
// 4. Check for updates (should show no changes)
// 5. Push a change to git server
// 6. Check for updates (should show changes available)
// 7. Sync again (should update files)
// 8. Add a file, then remove it in git
// 9. Sync with clean (should remove stale file)
func TestMonitoringWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := NewTestContainer(t, "Dockerfile.ubuntu")
	workDir := "/work/stevedore"

	// Copy sources and set up environment
	tc.CopySourcesToWorkDir(workDir)

	stateDir := filepath.Join(tc.StateHostPath, "stevedore-state")
	env := map[string]string{
		"STEVEDORE_HOST_ROOT":           stateDir,
		"STEVEDORE_CONTAINER_NAME":      tc.StevedoreContainerName,
		"STEVEDORE_IMAGE":               tc.StevedoreImageTag,
		"STEVEDORE_ASSUME_YES":          "1",
		"STEVEDORE_BOOTSTRAP_SELF":      "0",
		"STEVEDORE_ALLOW_UPSTREAM_MAIN": "1",
	}

	// Step 1: Install stevedore
	t.Log("Step 1: Installing stevedore...")
	tc.ExecBashOKTimeout(env, fmt.Sprintf("cd %s && ./stevedore-install.sh", workDir), 10*time.Minute)

	// Step 2: Set up git server
	t.Log("Step 2: Setting up git server...")
	gs := NewGitServer(t)

	deploymentName := "monitor-test"
	gitURL := gs.GetSshUrl(deploymentName)

	// Step 3: Initialize repo with initial content
	t.Log("Step 3: Creating initial repository...")
	err := gs.InitRepoWithContent(deploymentName, map[string]string{
		"docker-compose.yaml": `services:
  web:
    image: alpine:latest
    command: ["sleep", "infinity"]
`,
		"config.txt": "version=1.0",
		"data.txt":   "initial data",
	})
	if err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	// Step 4: Add deployment to stevedore
	t.Log("Step 4: Adding deployment to stevedore...")
	output := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh repo add %s %s --branch main
	`, workDir, tc.StevedoreContainerName, deploymentName, gitURL))

	// Extract public key
	publicKey := ""
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "ssh-ed25519") {
			publicKey = strings.TrimSpace(line)
			break
		}
	}
	if publicKey == "" {
		t.Fatal("Failed to extract public key")
	}

	// Add key to git server
	if err := gs.AddAuthorizedKey(publicKey); err != nil {
		t.Fatalf("failed to add authorized key: %v", err)
	}

	// Step 5: Initial sync
	t.Log("Step 5: Performing initial sync...")
	syncOutput := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy sync %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("Initial sync output:\n%s", syncOutput)

	if !strings.Contains(syncOutput, "Repository synced") {
		t.Errorf("Expected 'Repository synced' in output")
	}

	// Verify files exist
	tc.ExecOK("test", "-f", fmt.Sprintf("%s/deployments/%s/repo/git/config.txt", stateDir, deploymentName))
	tc.ExecOK("test", "-f", fmt.Sprintf("%s/deployments/%s/repo/git/data.txt", stateDir, deploymentName))

	// Step 6: Check for updates (should show no changes)
	t.Log("Step 6: Checking for updates (expecting none)...")
	checkOutput := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh check %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("Check output (no changes):\n%s", checkOutput)

	if !strings.Contains(checkOutput, "Up to date") {
		t.Errorf("Expected 'Up to date' in check output, got: %s", checkOutput)
	}

	// Step 7: Push a change to git server
	t.Log("Step 7: Pushing change to git server...")
	err = gs.UpdateFile(deploymentName, "config.txt", "version=2.0")
	if err != nil {
		t.Fatalf("failed to update file: %v", err)
	}

	// Step 8: Check for updates (should show changes)
	t.Log("Step 8: Checking for updates (expecting changes)...")
	checkOutput2 := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh check %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("Check output (with changes):\n%s", checkOutput2)

	if !strings.Contains(checkOutput2, "Updates available") {
		t.Errorf("Expected 'Updates available' in check output, got: %s", checkOutput2)
	}

	// Step 9: Sync to get the update
	t.Log("Step 9: Syncing to get update...")
	syncOutput2 := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy sync %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("Sync output:\n%s", syncOutput2)

	// Verify updated content
	configContent := tc.ExecBashOK(nil, fmt.Sprintf(
		"cat %s/deployments/%s/repo/git/config.txt",
		stateDir, deploymentName,
	))
	if !strings.Contains(configContent, "version=2.0") {
		t.Errorf("Expected updated config content, got: %s", configContent)
	}

	// Step 10: Add a file then remove it (test stale file cleanup)
	t.Log("Step 10: Testing stale file cleanup...")

	// Add a new file
	err = gs.UpdateFile(deploymentName, "temp-file.txt", "temporary content")
	if err != nil {
		t.Fatalf("failed to add temp file: %v", err)
	}

	// Sync to get the new file
	tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy sync %s
	`, workDir, tc.StevedoreContainerName, deploymentName))

	// Verify temp file exists
	tc.ExecOK("test", "-f", fmt.Sprintf("%s/deployments/%s/repo/git/temp-file.txt", stateDir, deploymentName))

	// Remove the file from git
	err = gs.DeleteFile(deploymentName, "temp-file.txt")
	if err != nil {
		t.Fatalf("failed to delete temp file: %v", err)
	}

	// Sync with clean (default behavior)
	t.Log("Step 11: Syncing with stale file cleanup...")
	syncOutput3 := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy sync %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("Sync with cleanup output:\n%s", syncOutput3)

	// Verify temp file is removed
	exitCode := tc.ExecBashExitCode(nil, fmt.Sprintf(
		"test -f %s/deployments/%s/repo/git/temp-file.txt",
		stateDir, deploymentName,
	))
	if exitCode == 0 {
		t.Error("Expected temp-file.txt to be removed after sync with clean")
	}

	// Step 12: Test --no-clean flag
	t.Log("Step 12: Testing --no-clean flag...")

	// Add another file
	err = gs.UpdateFile(deploymentName, "keep-file.txt", "should stay")
	if err != nil {
		t.Fatalf("failed to add keep file: %v", err)
	}

	// Sync to get it
	tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy sync %s
	`, workDir, tc.StevedoreContainerName, deploymentName))

	// Create a local-only file (simulate stale file)
	tc.ExecBashOK(nil, fmt.Sprintf(
		"echo 'local only' > %s/deployments/%s/repo/git/local-only.txt",
		stateDir, deploymentName,
	))

	// Remove keep-file from git
	err = gs.DeleteFile(deploymentName, "keep-file.txt")
	if err != nil {
		t.Fatalf("failed to delete keep file: %v", err)
	}

	// Sync with --no-clean
	tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy sync %s --no-clean
	`, workDir, tc.StevedoreContainerName, deploymentName))

	// Verify local-only file still exists (not cleaned)
	tc.ExecOK("test", "-f", fmt.Sprintf("%s/deployments/%s/repo/git/local-only.txt", stateDir, deploymentName))

	t.Log("Monitoring workflow test completed successfully!")
}
