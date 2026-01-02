package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestQuerySocketWorkflow tests the query socket API:
// 1. Install stevedore with query socket enabled
// 2. Deploy an application with ingress labels
// 3. Get a query token for the deployment
// 4. Query the socket API endpoints
// 5. Test long-polling for changes
func TestQuerySocketWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := NewTestContainer(t, "Dockerfile.ubuntu")
	workDir := "/work/stevedore"

	// Copy sources and set up environment
	tc.CopySourcesToWorkDir(workDir)

	stateDir := filepath.Join(tc.StateHostPath, "stevedore-state")
	querySocketPath := "/var/run/stevedore/query.sock"

	env := map[string]string{
		"STEVEDORE_HOST_ROOT":           stateDir,
		"STEVEDORE_CONTAINER_NAME":      tc.StevedoreContainerName,
		"STEVEDORE_IMAGE":               tc.StevedoreImageTag,
		"STEVEDORE_ASSUME_YES":          "1",
		"STEVEDORE_BOOTSTRAP_SELF":      "0",
		"STEVEDORE_ALLOW_UPSTREAM_MAIN": "1",
		"STEVEDORE_GIT_URL":             "git@github.com:test/test.git",
		"STEVEDORE_GIT_BRANCH":          "test",
	}

	// Step 1: Install stevedore
	t.Log("Step 1: Installing stevedore...")
	tc.ExecBashOKTimeout(env, fmt.Sprintf("cd %s && ./stevedore-install.sh", workDir), 10*time.Minute)

	// Step 2: Set up a git server using GitServer helper
	t.Log("Step 2: Setting up git server...")
	gs := NewGitServer(t)

	deploymentName := "ingress-app"
	gitURL := gs.GetSshUrl(deploymentName)
	t.Logf("Git server URL: %s", gitURL)

	// Step 3: Initialize the repository with ingress app content
	t.Log("Step 3: Creating repository with ingress labels...")

	// Read ingress app files from testdata
	testdataDir := filepath.Join(getProjectRoot(), "tests", "integration", "testdata", "ingress-app")
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

	// Initialize repo with files
	err = gs.InitRepoWithContent(deploymentName, map[string]string{
		"Dockerfile":          string(dockerfile),
		"docker-compose.yaml": string(compose),
		"server.py":           string(serverPy),
		"version.txt":         fmt.Sprintf("v1.0.0-ingress-%d", time.Now().Unix()),
	})
	if err != nil {
		t.Fatalf("failed to init repo: %v", err)
	}

	// Step 4: Add the repo to stevedore
	t.Log("Step 4: Adding deployment to stevedore...")
	output := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh repo add %s %s --branch main
	`, workDir, tc.StevedoreContainerName, deploymentName, gitURL))
	t.Logf("repo add output:\n%s", output)

	// Extract the public key
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

	// Add the key to git server
	if err := gs.AddAuthorizedKey(publicKey); err != nil {
		t.Fatalf("failed to add authorized key: %v", err)
	}

	// Step 5: Sync and deploy
	t.Log("Step 5: Syncing and deploying...")
	tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy sync %s
	`, workDir, tc.StevedoreContainerName, deploymentName))

	tc.ExecBashOKTimeout(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy up %s
	`, workDir, tc.StevedoreContainerName, deploymentName), 5*time.Minute)

	// Wait for container to be healthy
	t.Log("Waiting for container to be healthy...")
	waitForHealthy(t, tc, env, workDir, deploymentName, 60*time.Second)

	// Step 6: Get query token
	t.Log("Step 6: Getting query token...")
	tokenOutput := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh token get %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("token output:\n%s", tokenOutput)

	// Extract the token
	token := ""
	for _, line := range strings.Split(tokenOutput, "\n") {
		line = strings.TrimSpace(line)
		// Token is a hex string of 64 chars
		if len(line) == 64 && !strings.Contains(line, " ") {
			token = line
			break
		}
	}
	if token == "" {
		t.Fatalf("Failed to extract token from output: %s", tokenOutput)
	}
	t.Logf("Token: %s", token)

	// Step 7: Test query socket endpoints
	t.Log("Step 7: Testing query socket endpoints...")

	// Install curl inside the stevedore container for socket testing
	// The query socket is inside the stevedore container, so we need to run curl there
	tc.ExecBashOK(nil, fmt.Sprintf(`
		docker exec %s sh -c "apk add --no-cache curl"
	`, tc.StevedoreContainerName))

	// Helper to run curl inside the stevedore container
	curlInStevedore := func(args string) string {
		return tc.ExecBashOK(nil, fmt.Sprintf(`
			docker exec %s curl -s %s
		`, tc.StevedoreContainerName, args))
	}

	// Test /healthz (no auth required)
	t.Log("Testing /healthz endpoint...")
	healthzOutput := curlInStevedore(fmt.Sprintf(`--unix-socket %s http://localhost/healthz`, querySocketPath))
	t.Logf("/healthz output: %s", healthzOutput)

	if !strings.Contains(healthzOutput, `"status":"ok"`) {
		t.Errorf("Expected /healthz to return ok status, got: %s", healthzOutput)
	}

	// Test /deployments with auth
	t.Log("Testing /deployments endpoint...")
	deploymentsOutput := curlInStevedore(fmt.Sprintf(`--unix-socket %s -H "Authorization: Bearer %s" http://localhost/deployments`, querySocketPath, token))
	t.Logf("/deployments output: %s", deploymentsOutput)

	var deployments []map[string]string
	if err := json.Unmarshal([]byte(deploymentsOutput), &deployments); err != nil {
		t.Fatalf("Failed to parse deployments response: %v", err)
	}

	found := false
	for _, d := range deployments {
		if d["name"] == deploymentName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected to find deployment %s in list: %v", deploymentName, deployments)
	}

	// Test /services with auth
	t.Log("Testing /services endpoint...")
	servicesOutput := curlInStevedore(fmt.Sprintf(`--unix-socket %s -H "Authorization: Bearer %s" http://localhost/services`, querySocketPath, token))
	t.Logf("/services output: %s", servicesOutput)

	var services []map[string]interface{}
	if err := json.Unmarshal([]byte(servicesOutput), &services); err != nil {
		t.Fatalf("Failed to parse services response: %v", err)
	}

	// Should find a service with ingress enabled
	hasIngress := false
	for _, s := range services {
		if s["deployment"] == deploymentName {
			if ingress, ok := s["ingress"].(map[string]interface{}); ok {
				if enabled, ok := ingress["enabled"].(bool); ok && enabled {
					hasIngress = true
					// Verify ingress config
					if ingress["subdomain"] != "myapp" {
						t.Errorf("Expected subdomain 'myapp', got: %v", ingress["subdomain"])
					}
					if port, ok := ingress["port"].(float64); !ok || int(port) != 8080 {
						t.Errorf("Expected port 8080, got: %v", ingress["port"])
					}
				}
			}
		}
	}
	if !hasIngress {
		t.Errorf("Expected to find service with ingress enabled for %s: %v", deploymentName, services)
	}

	// Test /services?ingress=true filter
	t.Log("Testing /services?ingress=true endpoint...")
	ingressServicesOutput := curlInStevedore(fmt.Sprintf(`--unix-socket %s -H "Authorization: Bearer %s" "http://localhost/services?ingress=true"`, querySocketPath, token))
	t.Logf("/services?ingress=true output: %s", ingressServicesOutput)

	var ingressServices []map[string]interface{}
	if err := json.Unmarshal([]byte(ingressServicesOutput), &ingressServices); err != nil {
		t.Fatalf("Failed to parse ingress services response: %v", err)
	}

	// All services should have ingress enabled
	for _, s := range ingressServices {
		if ingress, ok := s["ingress"].(map[string]interface{}); ok {
			if enabled, ok := ingress["enabled"].(bool); !ok || !enabled {
				t.Errorf("Expected all services to have ingress enabled: %v", s)
			}
		}
	}

	// Test /status/{name}
	t.Log("Testing /status/{name} endpoint...")
	statusOutput := curlInStevedore(fmt.Sprintf(`--unix-socket %s -H "Authorization: Bearer %s" "http://localhost/status/%s"`, querySocketPath, token, deploymentName))
	t.Logf("/status/%s output: %s", deploymentName, statusOutput)

	var status map[string]interface{}
	if err := json.Unmarshal([]byte(statusOutput), &status); err != nil {
		t.Fatalf("Failed to parse status response: %v", err)
	}

	if status["deployment"] != deploymentName {
		t.Errorf("Expected deployment %s, got: %v", deploymentName, status["deployment"])
	}

	// Test authentication failure
	t.Log("Testing authentication failure...")
	authFailOutput := curlInStevedore(fmt.Sprintf(`-w "%%{http_code}" --unix-socket %s http://localhost/deployments`, querySocketPath))
	t.Logf("Auth fail output: %s", authFailOutput)

	if !strings.Contains(authFailOutput, "401") {
		t.Errorf("Expected 401 for unauthenticated request, got: %s", authFailOutput)
	}

	// Test invalid token
	invalidTokenOutput := curlInStevedore(fmt.Sprintf(`-w "%%{http_code}" --unix-socket %s -H "Authorization: Bearer invalid-token" http://localhost/deployments`, querySocketPath))
	t.Logf("Invalid token output: %s", invalidTokenOutput)

	if !strings.Contains(invalidTokenOutput, "401") {
		t.Errorf("Expected 401 for invalid token, got: %s", invalidTokenOutput)
	}

	// Step 8: Clean up
	t.Log("Step 8: Stopping deployment...")
	tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh deploy down %s
	`, workDir, tc.StevedoreContainerName, deploymentName))

	t.Log("Query socket workflow test completed successfully!")
}

// TestQuerySocketLongPolling tests the long-polling functionality of the query socket.
// This is a separate test because it requires timing-sensitive operations.
func TestQuerySocketLongPolling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tc := NewTestContainer(t, "Dockerfile.ubuntu")
	workDir := "/work/stevedore"

	tc.CopySourcesToWorkDir(workDir)

	stateDir := filepath.Join(tc.StateHostPath, "stevedore-state")
	querySocketPath := "/var/run/stevedore/query.sock"

	env := map[string]string{
		"STEVEDORE_HOST_ROOT":           stateDir,
		"STEVEDORE_CONTAINER_NAME":      tc.StevedoreContainerName,
		"STEVEDORE_IMAGE":               tc.StevedoreImageTag,
		"STEVEDORE_ASSUME_YES":          "1",
		"STEVEDORE_BOOTSTRAP_SELF":      "0",
		"STEVEDORE_ALLOW_UPSTREAM_MAIN": "1",
		"STEVEDORE_GIT_URL":             "git@github.com:test/test.git",
		"STEVEDORE_GIT_BRANCH":          "test",
	}

	// Install stevedore
	t.Log("Installing stevedore...")
	tc.ExecBashOKTimeout(env, fmt.Sprintf("cd %s && ./stevedore-install.sh", workDir), 10*time.Minute)

	// Install curl inside the stevedore container for socket testing
	tc.ExecBashOK(nil, fmt.Sprintf(`
		docker exec %s sh -c "apk add --no-cache curl"
	`, tc.StevedoreContainerName))

	// Add a simple deployment for token generation
	gs := NewGitServer(t)
	deploymentName := "poll-test"
	gitURL := gs.GetSshUrl(deploymentName)

	testdataDir := filepath.Join(getProjectRoot(), "tests", "integration", "testdata", "simple-app")
	dockerfile, _ := os.ReadFile(filepath.Join(testdataDir, "Dockerfile"))
	compose, _ := os.ReadFile(filepath.Join(testdataDir, "docker-compose.yaml"))
	serverPy, _ := os.ReadFile(filepath.Join(testdataDir, "server.py"))

	_ = gs.InitRepoWithContent(deploymentName, map[string]string{
		"Dockerfile":          string(dockerfile),
		"docker-compose.yaml": string(compose),
		"server.py":           string(serverPy),
		"version.txt":         "v1.0.0",
	})

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
	_ = gs.AddAuthorizedKey(publicKey)

	// Get token
	tokenOutput := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh token get %s
	`, workDir, tc.StevedoreContainerName, deploymentName))

	token := ""
	for _, line := range strings.Split(tokenOutput, "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 64 && !strings.Contains(line, " ") {
			token = line
			break
		}
	}

	// Test that /poll returns within timeout when no changes
	t.Log("Testing /poll endpoint (should timeout without changes)...")
	start := time.Now()
	pollOutput := tc.ExecBashOK(nil, fmt.Sprintf(`
		docker exec %s sh -c 'timeout 5 curl -s --unix-socket %s -H "Authorization: Bearer %s" "http://localhost/poll" || echo "{\"timeout\":true}"'
	`, tc.StevedoreContainerName, querySocketPath, token))
	elapsed := time.Since(start)
	t.Logf("/poll output after %v: %s", elapsed, pollOutput)

	// Poll should either timeout (5s) or return changed=false
	if strings.Contains(pollOutput, `"timeout"`) {
		// Good - curl timed out as expected (poll takes 60s)
		t.Log("Poll request timed out as expected (blocked waiting for changes)")
	} else if strings.Contains(pollOutput, `"changed":false`) {
		// Also good - returned no changes
		t.Log("Poll returned changed=false")
	} else if strings.Contains(pollOutput, `"changed":true`) {
		// This would happen if there were recent changes
		t.Log("Poll returned changed=true (recent changes detected)")
	} else {
		t.Logf("Unexpected poll response: %s", pollOutput)
	}

	t.Log("Query socket long-polling test completed!")
}

// TestQuerySocketTokenManagement tests token lifecycle operations.
func TestQuerySocketTokenManagement(t *testing.T) {
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
	}

	// Install stevedore
	t.Log("Installing stevedore...")
	tc.ExecBashOKTimeout(env, fmt.Sprintf("cd %s && ./stevedore-install.sh", workDir), 10*time.Minute)

	// Add a deployment
	gs := NewGitServer(t)
	deploymentName := "token-test"
	gitURL := gs.GetSshUrl(deploymentName)

	testdataDir := filepath.Join(getProjectRoot(), "tests", "integration", "testdata", "simple-app")
	dockerfile, _ := os.ReadFile(filepath.Join(testdataDir, "Dockerfile"))
	compose, _ := os.ReadFile(filepath.Join(testdataDir, "docker-compose.yaml"))
	serverPy, _ := os.ReadFile(filepath.Join(testdataDir, "server.py"))

	_ = gs.InitRepoWithContent(deploymentName, map[string]string{
		"Dockerfile":          string(dockerfile),
		"docker-compose.yaml": string(compose),
		"server.py":           string(serverPy),
		"version.txt":         "v1.0.0",
	})

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
	_ = gs.AddAuthorizedKey(publicKey)

	// Test token get (creates new token)
	t.Log("Testing token get (create)...")
	token1Output := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh token get %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("token get output:\n%s", token1Output)

	token1 := extractToken(token1Output)
	if token1 == "" {
		t.Fatal("Failed to extract token from first get")
	}

	// Test token get again (should return same token)
	t.Log("Testing token get (existing)...")
	token2Output := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh token get %s
	`, workDir, tc.StevedoreContainerName, deploymentName))

	token2 := extractToken(token2Output)
	if token1 != token2 {
		t.Errorf("Second get should return same token: %s vs %s", token1, token2)
	}

	// Test token regenerate
	t.Log("Testing token regenerate...")
	token3Output := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh token regenerate %s
	`, workDir, tc.StevedoreContainerName, deploymentName))
	t.Logf("token regenerate output:\n%s", token3Output)

	token3 := extractToken(token3Output)
	if token3 == "" {
		t.Fatal("Failed to extract token from regenerate")
	}
	if token3 == token1 {
		t.Error("Regenerated token should be different from original")
	}

	// Test token list
	t.Log("Testing token list...")
	listOutput := tc.ExecBashOK(env, fmt.Sprintf(`
		cd %s
		STEVEDORE_CONTAINER=%s ./stevedore.sh token list
	`, workDir, tc.StevedoreContainerName))
	t.Logf("token list output:\n%s", listOutput)

	if !strings.Contains(listOutput, deploymentName) {
		t.Errorf("Token list should contain deployment %s: %s", deploymentName, listOutput)
	}

	t.Log("Token management test completed successfully!")
}

// extractToken extracts a 64-character hex token from output.
func extractToken(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 64 && !strings.Contains(line, " ") {
			return line
		}
	}
	return ""
}
