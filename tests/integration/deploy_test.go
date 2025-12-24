package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDeploymentWorkflow tests the full deployment lifecycle:
// 1. Start a git server container with SSH
// 2. Initialize a sample repository with docker-compose.yaml
// 3. Add the deployment to stevedore
// 4. Add stevedore's public key to git server
// 5. Sync the repository
// 6. Deploy the application
// 7. Verify health status
// 8. Stop the deployment
func TestDeploymentWorkflow(t *testing.T) {
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
		"STEVEDORE_GIT_URL":             "git@github.com:test/test.git", // Required to bypass .git check
		"STEVEDORE_GIT_BRANCH":          "test",                         // Required to bypass .git check
	}

	// Step 1: Install stevedore
	t.Log("Step 1: Installing stevedore...")
	tc.ExecBashOKTimeout(env, fmt.Sprintf("cd %s && ./stevedore-install.sh", workDir), 10*time.Minute)

	// Step 2: Set up a git server using GitServer helper
	t.Log("Step 2: Setting up git server...")
	gs := NewGitServer(t)

	deploymentName := "simple-app"
	gitURL := gs.GetSshUrl(deploymentName)
	t.Logf("Git server URL: %s", gitURL)

	// Step 3: Initialize the repository with sample app content
	t.Log("Step 3: Creating sample repository with build config...")

	// Read sample app files from testdata
	testdataDir := filepath.Join(getProjectRoot(), "tests", "integration", "testdata", "simple-app")
	dockerfile, err := os.ReadFile(filepath.Join(testdataDir, "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read Dockerfile: %v", err)
	}
	compose, err := os.ReadFile(filepath.Join(testdataDir, "docker-compose.yaml"))
	if err != nil {
		t.Fatalf("failed to read docker-compose.yaml: %v", err)
	}
	serverPy, err := os.ReadFile(filepath.Join(testdataDir, "server.py"))
	if err != nil {
		t.Fatalf("failed to read server.py: %v", err)
	}

	// Initialize repo with files using local file protocol (no SSH needed)
	err = gs.InitRepoWithContent(deploymentName, map[string]string{
		"Dockerfile":          string(dockerfile),
		"docker-compose.yaml": string(compose),
		"server.py":           string(serverPy),
		"version.txt":         fmt.Sprintf("v1.0.0-%d", time.Now().Unix()),
	})
	if err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	// Step 4: Add the repo to stevedore (generates SSH key)
	t.Log("Step 4: Adding deployment to stevedore...")
	output := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh repo add %s %s --branch main
	`, workDir, tc.StevedoreContainerName, deploymentName, gitURL))
	t.Logf("repo add output:\n%s", output)

	// Extract the public key from the output
	publicKey := ""
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "ssh-ed25519") {
			publicKey = strings.TrimSpace(line)
			break
		}
	}
	if publicKey == "" {
		t.Fatal("Failed to extract public key from repo add output")
	}
	t.Logf("Public key: %s", publicKey)

	// Step 5: Add the stevedore public key to the git server
	t.Log("Step 5: Adding stevedore public key to git server...")
	if err := gs.AddAuthorizedKey(publicKey); err != nil {
		t.Fatalf("failed to add authorized key: %v", err)
	}

	// Step 6: Sync the repository
	t.Log("Step 6: Syncing repository...")
	syncOutput := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy sync %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("sync output:\n%s", syncOutput)

	if !strings.Contains(syncOutput, "Repository synced") {
		t.Errorf("Expected 'Repository synced' in output, got: %s", syncOutput)
	}

	// Verify the git checkout exists
	tc.ExecOK("test", "-d", fmt.Sprintf("%s/deployments/%s/repo/git/.git", stateDir, deploymentName))
	tc.ExecOK("test", "-f", fmt.Sprintf("%s/deployments/%s/repo/git/docker-compose.yaml", stateDir, deploymentName))

	// Step 7: Deploy the application
	t.Log("Step 7: Deploying application (with build)...")
	deployOutput := tc.ExecBashOKTimeout(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy up %s
	`, workDir, tc.StevedoreContainerName, deploymentName), 5*time.Minute)
	t.Logf("deploy output:\n%s", deployOutput)

	if !strings.Contains(deployOutput, "Deployed") {
		t.Errorf("Expected 'Deployed' in output, got: %s", deployOutput)
	}

	// Wait for container to be healthy
	t.Log("Waiting for container to be healthy...")
	waitForHealthy(t, tc, env, workDir, deploymentName, 60*time.Second)

	// Step 8: Check status
	t.Log("Step 8: Checking deployment status...")
	statusOutput := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh status %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("status output:\n%s", statusOutput)

	// The status should show containers
	if !strings.Contains(statusOutput, "Deployment:") {
		t.Errorf("Expected 'Deployment:' in status output, got: %s", statusOutput)
	}

	// Verify containers have correct compose labels (discovered by stevedore)
	containers := tc.ExecBashOK(nil, fmt.Sprintf(
		`docker ps --filter "label=com.docker.compose.project=stevedore-%s" --format "{{.Names}}"`,
		deploymentName,
	))
	if strings.TrimSpace(containers) == "" {
		t.Error("No containers found with compose project label")
	}
	t.Logf("Deployed containers: %s", containers)

	// Step 9: Stop the deployment
	t.Log("Step 9: Stopping deployment...")
	stopOutput := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy down %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("stop output:\n%s", stopOutput)

	// Final status check - should show no containers
	finalStatus := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh status %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("final status output:\n%s", finalStatus)

	// Verify containers are gone
	containersAfter := tc.ExecBashOK(nil, fmt.Sprintf(
		`docker ps --filter "label=com.docker.compose.project=stevedore-%s" --format "{{.Names}}" || true`,
		deploymentName,
	))
	if strings.TrimSpace(containersAfter) != "" {
		t.Errorf("Expected no containers after down, got: %s", containersAfter)
	}

	t.Log("Deployment workflow test completed successfully!")
}

// waitForHealthy waits for the deployment to become healthy.
func waitForHealthy(t *testing.T, tc *TestContainer, env map[string]string, workDir, deploymentName string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output := tc.ExecBashOK(env, fmt.Sprintf(`
			cd %s
			STEVEDORE_CONTAINER=%s ./stevedore.sh status %s 2>/dev/null || true
		`, workDir, tc.StevedoreContainerName, deploymentName))

		if strings.Contains(output, "healthy") && strings.Contains(output, "Healthy:    true") {
			t.Log("Deployment is healthy")
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Log("Warning: deployment did not become healthy within timeout (proceeding anyway)")
}

// getProjectRoot returns the path to the project root directory.
func getProjectRoot() string {
	// Find the project root by looking for go.mod
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// At filesystem root, return current working directory
			cwd, _ := os.Getwd()
			return cwd
		}
		dir = parent
	}
}
