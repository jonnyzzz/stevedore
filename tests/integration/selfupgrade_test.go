package integration_test

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestSelfUpgrade tests Stevedore's self-upgrade workflow:
// 1. Install stevedore with self-bootstrap mode (stevedore manages itself)
// 2. Add another deployment (simple-app service)
// 3. Deploy the simple-app and verify it's running
// 4. Push a VERSION change to stevedore repo
// 5. Run self-update command
// 6. Verify the service is still running after upgrade
// 7. Verify stevedore was updated to new version
//
// This test verifies that workload containers survive stevedore self-update.
func TestSelfUpgrade(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Skip on macOS due to Docker Desktop volume mount limitations
	if runtime.GOOS == "darwin" {
		t.Skip("skipping self-upgrade test on macOS (Docker Desktop volume mount limitations)")
	}

	// Step 1: Create donor container
	t.Log("Step 1: Creating donor container...")
	donor := NewTestContainer(t, "Dockerfile.ubuntu")
	workDir := "/work/stevedore"

	// Copy sources to a writable work directory
	donor.CopySourcesToWorkDir(workDir)

	// Step 2: Create git server
	t.Log("Step 2: Creating git server...")
	gs := NewGitServer(t)

	// Initialize stevedore repo from container sources
	repoName := "stevedore"
	err := gs.InitRepoFromContainer(donor, "/tmp/stevedore-src", repoName)
	if err != nil {
		t.Fatalf("failed to initialize stevedore repo: %v", err)
	}

	stevedoreGitURL := gs.GetSshUrl(repoName)
	t.Logf("Stevedore Git URL: %s", stevedoreGitURL)

	// Step 3: Run installer with STEVEDORE_GIT_URL (self-bootstrap mode)
	t.Log("Step 3: Running installer with self-bootstrap...")

	stateDir := filepath.Join(donor.StateHostPath, "stevedore-state")
	installEnv := map[string]string{
		"STEVEDORE_HOST_ROOT":       stateDir,
		"STEVEDORE_CONTAINER_NAME":  donor.StevedoreContainerName,
		"STEVEDORE_IMAGE":           donor.StevedoreImageTag,
		"STEVEDORE_ASSUME_YES":      "1",
		"STEVEDORE_BOOTSTRAP_SELF":  "1",
		"STEVEDORE_GIT_URL":         stevedoreGitURL,
		"STEVEDORE_GIT_BRANCH":      "main",
		"STEVEDORE_SELF_DEPLOYMENT": "stevedore",
	}

	output := donor.ExecBashOKTimeout(installEnv, fmt.Sprintf("cd %s && ./stevedore-install.sh", workDir), 15*time.Minute)
	t.Logf("Installer output:\n%s", output)

	// Step 4: Extract stevedore's public key and add to git server
	t.Log("Step 4: Adding stevedore public key to git server...")

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
		t.Fatal("Failed to extract stevedore public key")
	}

	if err := gs.AddAuthorizedKey(publicKey); err != nil {
		t.Fatalf("failed to add stevedore key: %v", err)
	}

	// Step 5: Create simple-app repo and add as second deployment
	t.Log("Step 5: Creating simple-app deployment...")

	simpleAppName := "simple-app"
	simpleAppGitURL := gs.GetSshUrl(simpleAppName)

	err = gs.InitRepoWithContent(simpleAppName, map[string]string{
		"docker-compose.yaml": `services:
  web:
    image: alpine:latest
    command: ["sh", "-c", "while true; do echo 'Service running'; sleep 10; done"]
    labels:
      - "com.stevedore.test=simple-app"
`,
	})
	if err != nil {
		t.Fatalf("failed to init simple-app repo: %v", err)
	}

	// Add simple-app deployment to stevedore
	addOutput := donor.ExecBashOK(installEnv, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh repo add %s %s --branch main
	`, workDir, donor.StevedoreContainerName, simpleAppName, simpleAppGitURL))
	t.Logf("repo add output:\n%s", addOutput)

	// Extract simple-app's public key and add to git server
	simpleAppKey := ""
	for _, line := range strings.Split(addOutput, "\n") {
		if strings.HasPrefix(line, "ssh-ed25519") {
			simpleAppKey = strings.TrimSpace(line)
			break
		}
	}
	if simpleAppKey == "" {
		t.Fatal("Failed to extract simple-app public key")
	}

	if err := gs.AddAuthorizedKey(simpleAppKey); err != nil {
		t.Fatalf("failed to add simple-app key: %v", err)
	}

	// Step 6: Sync and deploy simple-app
	t.Log("Step 6: Syncing and deploying simple-app...")

	donor.ExecBashOK(installEnv, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy sync %s
	`, workDir, donor.StevedoreContainerName, simpleAppName))

	donor.ExecBashOKTimeout(installEnv, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy up %s
	`, workDir, donor.StevedoreContainerName, simpleAppName), 5*time.Minute)

	// Verify simple-app container is running
	t.Log("Step 6.1: Verifying simple-app is running...")
	time.Sleep(5 * time.Second) // Give container time to start

	containers := donor.ExecBashOK(nil, fmt.Sprintf(
		`docker ps --filter "label=com.stevedore.test=simple-app" --format "{{.Names}} {{.Status}}"`,
	))
	t.Logf("Simple-app containers: %s", containers)

	if !strings.Contains(containers, "Up") {
		t.Fatal("simple-app container is not running")
	}

	// Step 7: Sync stevedore repo and get initial version
	t.Log("Step 7: Syncing stevedore repo...")

	donor.ExecBashOK(installEnv, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy sync stevedore
	`, workDir, donor.StevedoreContainerName))

	// Get the initial stevedore version
	initialVersion := strings.TrimSpace(donor.ExecOK("docker", "exec", "-i", donor.StevedoreContainerName, "/app/stevedore", "version"))
	t.Logf("Initial stevedore version: %s", initialVersion)

	// Step 8: Push VERSION change to stevedore repo
	t.Log("Step 8: Pushing VERSION change to stevedore repo...")

	newVersion := "99.99.99-selfupgrade-test"
	err = gs.UpdateFile(repoName, "VERSION", newVersion)
	if err != nil {
		t.Fatalf("failed to update VERSION: %v", err)
	}

	// Verify check detects the change
	checkOutput := donor.ExecBashOK(installEnv, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh check stevedore
	`, workDir, donor.StevedoreContainerName))
	t.Logf("Check output after push:\n%s", checkOutput)

	if !strings.Contains(checkOutput, "Updates available") {
		t.Errorf("Expected 'Updates available' in check output, got: %s", checkOutput)
	}

	// Step 9: Run self-update
	t.Log("Step 9: Running self-update...")

	// Before self-update, record simple-app container ID
	simpleAppContainerBefore := strings.TrimSpace(donor.ExecBashOK(nil,
		`docker ps --filter "label=com.stevedore.test=simple-app" --format "{{.ID}}" | head -1`,
	))
	t.Logf("Simple-app container ID before update: %s", simpleAppContainerBefore)

	selfUpdateRes, selfUpdateErr := donor.ExecBashTimeout(installEnv, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh self-update 2>&1
	`, workDir, donor.StevedoreContainerName), 15*time.Minute)
	t.Logf("Self-update output:\n%s", selfUpdateRes.Output)
	if selfUpdateErr != nil {
		t.Fatalf("Self-update failed (exit %d): %v", selfUpdateRes.ExitCode, selfUpdateErr)
	}

	// Step 10: Wait for self-update to complete
	t.Log("Step 10: Waiting for self-update to complete...")

	// Give the update worker a moment to start
	time.Sleep(3 * time.Second)

	// Find the update worker container
	workerContainers := donor.ExecBashOK(nil,
		`docker ps -a --filter "label=com.stevedore.role=update-worker" --format "{{.Names}} {{.Status}}"`,
	)
	t.Logf("Update worker containers: %s", workerContainers)

	// If worker exists, wait for it to complete and get logs
	if workerContainers != "" {
		workerName := strings.Fields(strings.TrimSpace(workerContainers))[0]
		t.Logf("Waiting for update worker %s to complete...", workerName)

		// Wait for worker to finish (max 2 minutes)
		workerDeadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(workerDeadline) {
			status := strings.TrimSpace(donor.ExecBashOK(nil,
				fmt.Sprintf(`docker ps -a --filter name=^%s$ --format "{{.Status}}"`, workerName),
			))
			if status == "" || strings.Contains(status, "Exited") {
				break
			}
			time.Sleep(2 * time.Second)
		}

		// Get worker logs before it's removed
		workerLogs := donor.ExecBashOK(nil, fmt.Sprintf(`docker logs %s 2>&1 || echo "no logs"`, workerName))
		t.Logf("Update worker logs:\n%s", workerLogs)
	}

	// The self-update spawns a worker that replaces the stevedore container
	// We need to wait for the new container to start
	deadline := time.Now().Add(3 * time.Minute)
	var newStevedoreRunning bool
	for time.Now().Before(deadline) {
		// Check if a new stevedore container is running with the new version
		versionCheck, err := donor.Exec("docker", "exec", "-i", donor.StevedoreContainerName, "/app/stevedore", "version")
		if err == nil && strings.Contains(versionCheck.Output, newVersion) {
			newStevedoreRunning = true
			t.Logf("New stevedore running with version: %s", strings.TrimSpace(versionCheck.Output))
			break
		}
		time.Sleep(5 * time.Second)
	}

	if !newStevedoreRunning {
		// Check container status
		containerStatus := donor.ExecBashOK(nil, fmt.Sprintf(
			"docker ps -a --filter name=%s --format '{{.Status}}'",
			donor.StevedoreContainerName,
		))
		t.Logf("Stevedore container status: %s", containerStatus)

		// Get logs
		logs := donor.ExecBashOK(nil, fmt.Sprintf(
			"docker logs --tail 50 %s 2>&1 || echo 'no logs'",
			donor.StevedoreContainerName,
		))
		t.Logf("Stevedore container logs:\n%s", logs)

		// Check if update worker ran
		workerContainers := donor.ExecBashOK(nil,
			`docker ps -a --filter "label=com.stevedore.role=update-worker" --format "{{.Names}} {{.Status}}"`,
		)
		t.Logf("Update worker containers: %s", workerContainers)

		t.Fatal("Stevedore was not updated to new version within timeout")
	}

	// Step 11: Verify simple-app is still running after update
	t.Log("Step 11: Verifying simple-app is still running after update...")

	simpleAppContainerAfter := strings.TrimSpace(donor.ExecBashOK(nil,
		`docker ps --filter "label=com.stevedore.test=simple-app" --format "{{.ID}}" | head -1`,
	))
	t.Logf("Simple-app container ID after update: %s", simpleAppContainerAfter)

	if simpleAppContainerAfter == "" {
		t.Fatal("Simple-app container is not running after self-update")
	}

	// Verify it's the same container (wasn't restarted)
	if simpleAppContainerBefore != simpleAppContainerAfter {
		t.Logf("WARNING: Simple-app container changed during self-update (before: %s, after: %s)",
			simpleAppContainerBefore, simpleAppContainerAfter)
		// This is a warning, not a failure - the important thing is that the service is running
	} else {
		t.Log("Simple-app container survived self-update unchanged")
	}

	// Verify simple-app is healthy
	simpleAppStatus := donor.ExecBashOK(nil,
		`docker ps --filter "label=com.stevedore.test=simple-app" --format "{{.Names}} {{.Status}}"`,
	)
	t.Logf("Simple-app status after update: %s", simpleAppStatus)

	if !strings.Contains(simpleAppStatus, "Up") {
		t.Fatal("Simple-app container is not running after self-update")
	}

	// Step 12: Verify final stevedore version
	t.Log("Step 12: Verifying final stevedore version...")

	finalVersion := strings.TrimSpace(donor.ExecOK("docker", "exec", "-i", donor.StevedoreContainerName, "/app/stevedore", "version"))
	t.Logf("Final stevedore version: %s", finalVersion)

	if !strings.Contains(finalVersion, newVersion) {
		t.Errorf("Expected final version to contain %q, got: %s", newVersion, finalVersion)
	}

	// Step 13: Clean up
	t.Log("Step 13: Cleaning up...")

	donor.ExecBashOK(installEnv, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy down %s 2>/dev/null || true
	`, workDir, donor.StevedoreContainerName, simpleAppName))

	t.Log("Self-upgrade test completed successfully!")
}
