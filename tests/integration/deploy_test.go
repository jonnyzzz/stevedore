//go:build integration

package integration_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDeploymentWorkflow tests the full deployment lifecycle:
// 1. Start a git server container with SSH
// 2. Initialize a sample repository with docker-compose.yaml
// 3. Add the deployment to stevedore
// 4. Sync the repository
// 5. Deploy the application
// 6. Verify health status
// 7. Stop the deployment
func TestDeploymentWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := NewTestContainer(t, "Dockerfile.ubuntu")
	workDir := "/tmp/stevedore-deploy-test"

	// Copy sources and set up environment
	tc.CopySourcesToWorkDir(workDir)

	stateDir := filepath.Join(tc.StateDir, "stevedore-state")
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

	// Step 2: Set up a git server container with SSH
	t.Log("Step 2: Setting up git server...")
	gitServerName := tc.ContainerPrefix + "-gitserver"
	gitRepoPath := "/git/test-repo.git"

	// Register cleanup for git server
	t.Cleanup(func() {
		tc.docker.stopAndRemoveContainer(gitServerName)
	})

	// Create git server container
	tc.ExecBashOK(nil, fmt.Sprintf(`
		# Create a simple git server container
		docker run -d --name %s \
			-v /tmp/git-repos:/git \
			alpine:latest sleep infinity

		# Install git and openssh
		docker exec %s apk add --no-cache git openssh-server openssh-keygen

		# Configure SSH
		docker exec %s sh -c 'ssh-keygen -A'
		docker exec %s sh -c 'mkdir -p /root/.ssh && chmod 700 /root/.ssh'
		docker exec %s sh -c 'echo "PermitRootLogin yes" >> /etc/ssh/sshd_config'
		docker exec %s sh -c 'echo "PasswordAuthentication no" >> /etc/ssh/sshd_config'
		docker exec %s sh -c 'echo "PubkeyAuthentication yes" >> /etc/ssh/sshd_config'
		docker exec %s sh -c 'echo "root:testpassword" | chpasswd'

		# Start SSH daemon
		docker exec %s /usr/sbin/sshd
	`, gitServerName, gitServerName, gitServerName, gitServerName, gitServerName, gitServerName, gitServerName, gitServerName, gitServerName))

	// Get git server IP
	gitServerIP := strings.TrimSpace(tc.ExecBashOK(nil, fmt.Sprintf(
		`docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' %s`,
		gitServerName,
	)))
	t.Logf("Git server IP: %s", gitServerIP)

	// Initialize a bare git repository
	tc.ExecBashOK(nil, fmt.Sprintf(`
		docker exec %s sh -c '
			mkdir -p %s
			cd %s
			git init --bare
			git config receive.denyCurrentBranch ignore
		'
	`, gitServerName, gitRepoPath, gitRepoPath))

	// Step 3: Create a sample repository with docker-compose.yaml
	t.Log("Step 3: Creating sample repository...")
	sampleRepoDir := "/tmp/sample-repo"
	tc.ExecBashOK(nil, fmt.Sprintf(`
		rm -rf %s
		mkdir -p %s
		cd %s

		# Create a simple docker-compose.yaml
		cat > docker-compose.yaml << 'COMPOSE_EOF'
services:
  web:
    image: nginx:alpine
    ports:
      - "8080:80"
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost/"]
      interval: 5s
      timeout: 3s
      retries: 3
COMPOSE_EOF

		# Initialize git repo and push
		git init
		git config user.email "test@test.com"
		git config user.name "Test"
		git add -A
		git commit -m "Initial commit"
	`, sampleRepoDir, sampleRepoDir, sampleRepoDir))

	// Step 4: Get the deploy key from stevedore and add it to git server
	t.Log("Step 4: Adding deployment and configuring SSH key...")
	deploymentName := "test-app"
	gitURL := fmt.Sprintf("ssh://root@%s%s", gitServerIP, gitRepoPath)

	// Add the repo to stevedore - this generates the SSH key
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

	// Add the public key to the git server
	tc.ExecBashOK(nil, fmt.Sprintf(`
		docker exec %s sh -c 'echo "%s" >> /root/.ssh/authorized_keys'
		docker exec %s sh -c 'chmod 600 /root/.ssh/authorized_keys'
	`, gitServerName, publicKey, gitServerName))

	// Push the sample repo to the git server
	tc.ExecBashOK(nil, fmt.Sprintf(`
		cd %s

		# Use the stevedore key for push (temporarily)
		export GIT_SSH_COMMAND="ssh -o StrictHostKeyChecking=no -i %s/deployments/%s/repo/ssh/id_ed25519"
		git remote add origin %s || git remote set-url origin %s
		git push -u origin main
	`, sampleRepoDir, stateDir, deploymentName, gitURL, gitURL))

	// Step 5: Sync the repository
	t.Log("Step 5: Syncing repository...")
	syncOutput := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy sync %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("sync output:\n%s", syncOutput)

	if !strings.Contains(syncOutput, "Repository synced") {
		t.Errorf("Expected 'Repository synced' in output, got: %s", syncOutput)
	}

	// Step 6: Deploy the application
	t.Log("Step 6: Deploying application...")
	deployOutput := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy up %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("deploy output:\n%s", deployOutput)

	if !strings.Contains(deployOutput, "Deployed") {
		t.Errorf("Expected 'Deployed' in output, got: %s", deployOutput)
	}

	// Wait for the container to be healthy
	time.Sleep(10 * time.Second)

	// Step 7: Check status
	t.Log("Step 7: Checking deployment status...")
	statusOutput := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh status %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("status output:\n%s", statusOutput)

	// The status should show containers
	if !strings.Contains(statusOutput, "Deployment:") {
		t.Errorf("Expected 'Deployment:' in status output, got: %s", statusOutput)
	}

	// Step 8: Stop the deployment
	t.Log("Step 8: Stopping deployment...")
	stopOutput := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy down %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("stop output:\n%s", stopOutput)

	if !strings.Contains(stopOutput, "Stopped") {
		t.Errorf("Expected 'Stopped' in output, got: %s", stopOutput)
	}

	// Final status check - should show no containers
	finalStatus := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh status %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("final status output:\n%s", finalStatus)

	t.Log("Deployment workflow test completed successfully!")
}
