package integration_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestInstaller_SelfBootstrap tests Stevedore's self-bootstrap mode where
// Stevedore manages its own repository. This test:
// 1. Pushes current Stevedore source files to the git server
// 2. Runs the installer with STEVEDORE_GIT_URL pointing to the git server
// 3. Verifies bootstrap created the stevedore deployment
// 4. Verifies the deployment can be synced
//
// Automatic updates are disabled for this test (STEVEDORE_POLL_INTERVAL=0).
func TestInstaller_SelfBootstrap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Step 1: Create donor container
	t.Log("Step 1: Creating donor container...")
	donor := NewTestContainer(t, "Dockerfile.ubuntu")
	workDir := "/work/stevedore"

	// Copy sources to a writable work directory
	donor.CopySourcesToWorkDir(workDir)

	// Step 2: Create git server and push Stevedore sources
	t.Log("Step 2: Creating git server and pushing Stevedore sources...")
	gs := NewGitServer(t)

	repoName := "stevedore"
	err := gs.InitRepoFromContainer(donor, "/tmp/stevedore-src", repoName)
	if err != nil {
		t.Fatalf("failed to initialize repo from container: %v", err)
	}

	gitURL := gs.GetSshUrl(repoName)
	t.Logf("Git URL for self-bootstrap: %s", gitURL)

	// Step 3: Run installer with STEVEDORE_GIT_URL
	t.Log("Step 3: Running installer with self-bootstrap...")

	stateDir := filepath.Join(donor.StateHostPath, "stevedore-state")
	installEnv := map[string]string{
		"STEVEDORE_HOST_ROOT":       stateDir,
		"STEVEDORE_CONTAINER_NAME":  donor.StevedoreContainerName,
		"STEVEDORE_IMAGE":           donor.StevedoreImageTag,
		"STEVEDORE_ASSUME_YES":      "1",
		"STEVEDORE_BOOTSTRAP_SELF":  "1", // Enable self-bootstrap
		"STEVEDORE_GIT_URL":         gitURL,
		"STEVEDORE_GIT_BRANCH":      "main",
		"STEVEDORE_SELF_DEPLOYMENT": "stevedore", // Name of the self-deployment
	}

	// Run installer
	output := donor.ExecBashOKTimeout(installEnv, fmt.Sprintf("cd %s && ./stevedore-install.sh", workDir), 15*time.Minute)
	t.Logf("Installer output:\n%s", output)

	// Verify installer used our git URL
	if !strings.Contains(output, "Using STEVEDORE_GIT_URL") {
		t.Error("Expected installer to log 'Using STEVEDORE_GIT_URL'")
	}

	// Step 4: Extract public key and add to git server
	t.Log("Step 4: Extracting public key and adding to git server...")

	// The bootstrap should have generated a key - extract it
	wrapperEnv := map[string]string{"STEVEDORE_CONTAINER": donor.StevedoreContainerName}
	keyOutput := donor.ExecEnvOK(wrapperEnv, "stevedore", "repo", "key", "stevedore")
	publicKey := ""
	for _, line := range strings.Split(keyOutput, "\n") {
		if strings.HasPrefix(line, "ssh-ed25519") {
			publicKey = strings.TrimSpace(line)
			break
		}
	}
	if publicKey == "" {
		t.Fatal("Failed to extract public key for stevedore deployment")
	}
	t.Logf("Public key: %s", publicKey[:50]+"...")

	// Add key to git server
	if err := gs.AddAuthorizedKey(publicKey); err != nil {
		t.Fatalf("failed to add authorized key: %v", err)
	}

	// Step 5: Verify the stevedore deployment exists
	t.Log("Step 5: Verifying stevedore deployment exists...")

	repoList := donor.ExecEnvOK(wrapperEnv, "stevedore", "repo", "list")
	if !containsLine(repoList, "stevedore") {
		t.Fatalf("Expected 'stevedore' deployment in repo list, got: %s", repoList)
	}
	t.Logf("Repo list:\n%s", repoList)

	// Verify the deployment directory was created
	deploymentDir := fmt.Sprintf("%s/deployments/stevedore", stateDir)
	donor.ExecOK("test", "-d", deploymentDir)

	// Step 6: Sync the repository
	t.Log("Step 6: Syncing stevedore repository...")

	syncOutput := donor.ExecBashOK(installEnv, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy sync stevedore
	`, workDir, donor.StevedoreContainerName))
	t.Logf("Sync output:\n%s", syncOutput)

	if !strings.Contains(syncOutput, "Repository synced") {
		t.Errorf("Expected 'Repository synced' in sync output")
	}

	// Verify key files exist in the synced repo
	repoDir := fmt.Sprintf("%s/deployments/stevedore/repo/git", stateDir)
	donor.ExecOK("test", "-f", fmt.Sprintf("%s/Dockerfile", repoDir))
	donor.ExecOK("test", "-f", fmt.Sprintf("%s/stevedore-install.sh", repoDir))
	donor.ExecOK("test", "-f", fmt.Sprintf("%s/VERSION", repoDir))

	// Step 7: Verify check for updates works
	t.Log("Step 7: Checking for updates (should show no changes)...")

	checkOutput := donor.ExecBashOK(installEnv, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh check stevedore
	`, workDir, donor.StevedoreContainerName))
	t.Logf("Check output:\n%s", checkOutput)

	if !strings.Contains(checkOutput, "Up to date") {
		t.Errorf("Expected 'Up to date' in check output, got: %s", checkOutput)
	}

	// Step 8: Push a change and verify check detects it
	t.Log("Step 8: Pushing a change and verifying detection...")

	err = gs.UpdateFile(repoName, "TEST_MARKER.txt", "self-bootstrap-test-marker")
	if err != nil {
		t.Fatalf("failed to update file: %v", err)
	}

	checkOutput2 := donor.ExecBashOK(installEnv, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh check stevedore
	`, workDir, donor.StevedoreContainerName))
	t.Logf("Check output after push:\n%s", checkOutput2)

	if !strings.Contains(checkOutput2, "Updates available") {
		t.Errorf("Expected 'Updates available' in check output, got: %s", checkOutput2)
	}

	// Step 9: Sync and verify the new file appears
	t.Log("Step 9: Syncing to get the update...")

	donor.ExecBashOK(installEnv, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy sync stevedore
	`, workDir, donor.StevedoreContainerName))

	// Verify the marker file exists
	markerContent := donor.ExecBashOK(nil, fmt.Sprintf("cat %s/TEST_MARKER.txt", repoDir))
	if !strings.Contains(markerContent, "self-bootstrap-test-marker") {
		t.Errorf("Expected marker file content, got: %s", markerContent)
	}

	t.Log("Self-bootstrap test completed successfully!")
}
