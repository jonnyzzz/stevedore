package integration_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestReconcileRestartsStoppedContainers verifies the daemon restarts a deployment
// when its containers are stopped (simulating a host reboot or crash).
func TestReconcileRestartsStoppedContainers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := NewTestContainer(t, "Dockerfile.ubuntu")
	workDir := "/work/stevedore"

	tc.CopySourcesToWorkDir(workDir)

	stateDir := filepath.Join(tc.StateHostPath, "stevedore-state")
	env := map[string]string{
		"STEVEDORE_HOST_ROOT":           stateDir,
		"STEVEDORE_CONTAINER_NAME":      tc.StevedoreContainerName,
		"STEVEDORE_IMAGE":               tc.StevedoreImageTag,
		"STEVEDORE_ASSUME_YES":          "1",
		"STEVEDORE_BOOTSTRAP_SELF":      "0",
		"STEVEDORE_ALLOW_UPSTREAM_MAIN": "1",
		"STEVEDORE_GIT_URL":             "git@github.com:test/test.git",
		"STEVEDORE_GIT_BRANCH":          "test",
		"STEVEDORE_RECONCILE_INTERVAL":  "2s",
	}

	t.Log("Step 1: Installing stevedore...")
	tc.ExecBashOKTimeout(env, fmt.Sprintf("cd %s && ./stevedore-install.sh", workDir), 10*time.Minute)

	t.Log("Step 2: Setting up git server...")
	gs := NewGitServer(t)

	deploymentName := "reconcile-test"
	gitURL := gs.GetSshUrl(deploymentName)

	t.Log("Step 3: Creating repository with docker-compose.yaml...")
	if err := gs.InitRepoWithContent(deploymentName, map[string]string{
		"docker-compose.yaml": `services:
  app:
    image: alpine:latest
    command: ["sleep", "infinity"]
`,
	}); err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	t.Log("Step 4: Adding deployment to stevedore...")
	output := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh repo add %s %s --branch main
	`, workDir, tc.StevedoreContainerName, deploymentName, gitURL))

	publicKey := ""
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "ssh-ed25519") {
			publicKey = strings.TrimSpace(line)
			break
		}
	}
	if publicKey == "" {
		t.Fatal("failed to extract public key from repo add output")
	}

	if err := gs.AddAuthorizedKey(publicKey); err != nil {
		t.Fatalf("failed to add authorized key: %v", err)
	}

	t.Log("Step 5: Syncing repository...")
	tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy sync %s
	`, workDir, tc.StevedoreContainerName, deploymentName))

	t.Log("Step 6: Deploying application...")
	tc.ExecBashOKTimeout(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy up %s
	`, workDir, tc.StevedoreContainerName, deploymentName), 5*time.Minute)

	waitForHealthyDeployment(t, tc, env, workDir, deploymentName, 60*time.Second)

	t.Log("Step 7: Stopping deployment containers (simulate reboot)...")
	stopCmd := fmt.Sprintf(
		`docker ps -q --filter "label=com.docker.compose.project=stevedore-%s" | xargs -r docker stop`,
		deploymentName,
	)
	tc.ExecBashOK(nil, stopCmd)

	stopped := tc.ExecBashOK(nil, fmt.Sprintf(
		`docker ps -q --filter "label=com.docker.compose.project=stevedore-%s"`,
		deploymentName,
	))
	if strings.TrimSpace(stopped) != "" {
		t.Fatalf("expected deployment containers to be stopped, still running: %s", stopped)
	}

	t.Log("Step 8: Waiting for daemon to reconcile and restart containers...")
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		running := tc.ExecBashOK(nil, fmt.Sprintf(
			`docker ps -q --filter "label=com.docker.compose.project=stevedore-%s"`,
			deploymentName,
		))
		if strings.TrimSpace(running) != "" {
			t.Log("containers restarted by daemon")
			return
		}
		time.Sleep(2 * time.Second)
	}

	status := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh status %s || true
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Fatalf("daemon did not restart containers in time; last status:\n%s", status)
}

func waitForHealthyDeployment(t *testing.T, tc *TestContainer, env map[string]string, workDir, deploymentName string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output := tc.ExecBashOK(env, fmt.Sprintf(`
			cd %s
			STEVEDORE_CONTAINER=%s ./stevedore.sh status %s 2>/dev/null || true
		`, workDir, tc.StevedoreContainerName, deploymentName))

		if strings.Contains(output, "Healthy:    true") {
			return
		}
		time.Sleep(2 * time.Second)
	}

	t.Log("Warning: deployment did not become healthy within timeout (proceeding anyway)")
}
