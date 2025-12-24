package stevedore

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SelfUpdateConfig holds configuration for self-update.
type SelfUpdateConfig struct {
	ContainerName string        // Name of the running stevedore container
	ImageTag      string        // Tag for the new image
	BuildTimeout  time.Duration // Timeout for image build (default: 15m)
	UpdateTimeout time.Duration // Timeout for update operation (default: 5m)
}

// SelfUpdate handles updating the stevedore container itself.
type SelfUpdate struct {
	instance *Instance
	config   SelfUpdateConfig
}

// NewSelfUpdate creates a new SelfUpdate instance.
func NewSelfUpdate(instance *Instance, config SelfUpdateConfig) *SelfUpdate {
	if config.ContainerName == "" {
		config.ContainerName = os.Getenv("STEVEDORE_CONTAINER_NAME")
		if config.ContainerName == "" {
			config.ContainerName = "stevedore"
		}
	}
	if config.BuildTimeout == 0 {
		config.BuildTimeout = 15 * time.Minute
	}
	if config.UpdateTimeout == 0 {
		config.UpdateTimeout = 5 * time.Minute
	}

	return &SelfUpdate{
		instance: instance,
		config:   config,
	}
}

// NeedsSelfUpdate checks if the stevedore deployment has a newer commit than currently running.
// Returns (needsUpdate, newCommit, error).
// If currentCommit is "unknown" or empty, returns (true, newCommit, nil) to force an update
// since we can't determine if we're up-to-date.
func (s *SelfUpdate) NeedsSelfUpdate(ctx context.Context, currentCommit string) (bool, string, error) {
	deployment := "stevedore"

	// Get the git directory for the stevedore deployment
	gitDir := filepath.Join(s.instance.DeploymentDir(deployment), "repo", "git")

	// Check if git directory exists
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return false, "", nil // No stevedore deployment configured
	}

	// Get the current HEAD commit from the checkout
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = gitDir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false, "", fmt.Errorf("get HEAD commit: %w", err)
	}

	newCommit := strings.TrimSpace(stdout.String())

	if currentCommit == "" || currentCommit == "unknown" {
		// Can't determine current version - force update to ensure we're up-to-date
		log.Printf("Current commit is unknown, forcing self-update to %s", newCommit[:12])
		return true, newCommit, nil
	}

	needsUpdate := newCommit != currentCommit
	return needsUpdate, newCommit, nil
}

// BuildNewImage builds a new stevedore image from the deployment checkout.
func (s *SelfUpdate) BuildNewImage(ctx context.Context) (string, error) {
	deployment := "stevedore"
	gitDir := filepath.Join(s.instance.DeploymentDir(deployment), "repo", "git")

	// Verify Dockerfile exists
	dockerfilePath := filepath.Join(gitDir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		return "", fmt.Errorf("Dockerfile not found in stevedore checkout: %s", dockerfilePath)
	}

	// Generate image tag with timestamp
	imageTag := s.config.ImageTag
	if imageTag == "" {
		imageTag = fmt.Sprintf("stevedore:update-%d", time.Now().Unix())
	}

	log.Printf("Building new stevedore image: %s", imageTag)

	ctx, cancel := context.WithTimeout(ctx, s.config.BuildTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "build", "-t", imageTag, ".")
	cmd.Dir = gitDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker build failed: %w: %s", err, stderr.String())
	}

	log.Printf("Built new stevedore image: %s", imageTag)
	return imageTag, nil
}

// Execute performs the self-update by spawning an update worker.
// The worker will:
// 1. Stop the current stevedore container
// 2. Start a new container with the new image
// 3. Clean up the old image
//
// NOTE: This method will cause the current process to exit!
func (s *SelfUpdate) Execute(ctx context.Context, newImageTag string) error {
	containerName := s.config.ContainerName

	log.Printf("Self-update: preparing to replace container %s with image %s", containerName, newImageTag)

	// Get the current container's mount for /opt/stevedore
	// Format: /host/path:/opt/stevedore:rw (or ro)
	mountsCmd := exec.CommandContext(ctx, "docker", "inspect", "--format",
		"{{range .Mounts}}{{if eq .Destination \"/opt/stevedore\"}}{{.Source}}{{end}}{{end}}",
		containerName)
	var mountsOut bytes.Buffer
	mountsCmd.Stdout = &mountsOut
	if err := mountsCmd.Run(); err != nil {
		return fmt.Errorf("inspect container mounts: %w", err)
	}
	hostRoot := strings.TrimSpace(mountsOut.String())
	if hostRoot == "" {
		// Fallback to default path if mount not found
		hostRoot = "/opt/stevedore"
	}
	log.Printf("Self-update: using host root: %s", hostRoot)

	// Get restart policy
	policyCmd := exec.CommandContext(ctx, "docker", "inspect", "--format",
		"{{.HostConfig.RestartPolicy.Name}}", containerName)
	var policyOut bytes.Buffer
	policyCmd.Stdout = &policyOut
	if err := policyCmd.Run(); err != nil {
		return fmt.Errorf("inspect restart policy: %w", err)
	}
	restartPolicy := strings.TrimSpace(policyOut.String())
	if restartPolicy == "" {
		restartPolicy = "unless-stopped"
	}

	// Get container env file path (on host)
	envFilePath := filepath.Join(hostRoot, "system", "container.env")

	// Create the update script that will run in a separate container
	// Note: LOG_FILE uses the container path since the script runs inside the worker
	updateScript := fmt.Sprintf(`#!/bin/sh
# Do not use set -e - we want to log all errors and continue
LOG_FILE="/stevedore-system/update.log"

log() {
  echo "$@"
  echo "$(date '+%%Y-%%m-%%d %%H:%%M:%%S') $@" >> "$LOG_FILE" 2>/dev/null || true
}

log "Self-update worker starting..."
log "Container name: %s"
log "New image: %s"
log "Host root: %s"
log "Env file: %s"
log "Restart policy: %s"

# Wait a moment for the original container to be ready for replacement
sleep 2

# Stop the current container
log "Stopping container: %s"
if docker stop %s; then
  log "Container stopped successfully"
else
  log "Warning: docker stop failed (may already be stopped)"
fi

# Remove the old container
log "Removing old container..."
if docker rm %s; then
  log "Container removed successfully"
else
  log "Warning: docker rm failed (may already be removed)"
fi

# Verify env file exists
if [ ! -f "%s" ]; then
  log "ERROR: Env file not found at %s"
  log "Listing system directory:"
  ls -la "$(dirname '%s')" >> "$LOG_FILE" 2>&1 || log "Could not list directory"
  exit 1
fi

# Start new container with the same configuration
log "Starting new container with image: %s"
if docker run -d \
  --name %s \
  --restart %s \
  --env-file %s \
  -p 42107:42107 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v %s:/opt/stevedore \
  %s \
  /app/stevedore -d; then
  log "New container started successfully"
else
  log "ERROR: Failed to start new container"
  log "Docker error:"
  docker run -d \
    --name %s \
    --restart %s \
    --env-file %s \
    -p 42107:42107 \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v %s:/opt/stevedore \
    %s \
    /app/stevedore -d 2>> "$LOG_FILE" || true
  exit 1
fi

log "Self-update complete!"
`, containerName, newImageTag, hostRoot, envFilePath, restartPolicy,
		containerName, containerName,
		containerName,
		envFilePath, envFilePath, envFilePath,
		newImageTag, containerName, restartPolicy, envFilePath, hostRoot, newImageTag,
		containerName, restartPolicy, envFilePath, hostRoot, newImageTag)

	// Run the update worker
	workerName := fmt.Sprintf("stevedore-update-%d", time.Now().Unix())

	log.Printf("Spawning update worker: %s", workerName)

	// Write script to temp file (the worker needs to access it)
	scriptPath := filepath.Join(s.instance.SystemDir(), "update-script.sh")
	if err := os.WriteFile(scriptPath, []byte(updateScript), 0755); err != nil {
		return fmt.Errorf("write update script: %w", err)
	}

	// Run the update worker container
	// Use hostRoot (the host path) for volume mount, not the container path
	// Mount as read-write so we can write the update.log for debugging
	hostSystemDir := hostRoot + "/system"
	args := []string{
		"run", "-d",
		"--name", workerName,
		"--rm",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", hostSystemDir + ":/stevedore-system:rw",
		"--label", "com.stevedore.managed=true",
		"--label", "com.stevedore.role=update-worker",
		"docker:cli",
		"sh", "-c", "cat /stevedore-system/update-script.sh | sh",
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("spawn update worker: %w: %s", err, stderr.String())
	}

	log.Printf("Update worker spawned: %s", workerName)
	log.Printf("Self-update initiated. This container will be replaced shortly.")

	return nil
}

// IsStevedoreDeployment returns true if the deployment is the stevedore self-deployment.
func IsStevedoreDeployment(name string) bool {
	return name == "stevedore"
}

// TriggerSelfUpdate performs a self-update if there are changes available.
// It syncs the stevedore deployment, builds a new image, and spawns an update worker.
// Returns (updated bool, error).
func (i *Instance) TriggerSelfUpdate(ctx context.Context, currentCommit string) (bool, error) {
	deployment := "stevedore"

	// Check if stevedore deployment exists
	deploymentDir := i.DeploymentDir(deployment)
	if _, err := os.Stat(deploymentDir); os.IsNotExist(err) {
		return false, fmt.Errorf("stevedore deployment not found (not in self-bootstrap mode)")
	}

	// Sync first to get latest changes
	log.Printf("Self-update: syncing stevedore deployment...")
	result, err := i.GitSyncClean(ctx, deployment, true)
	if err != nil {
		return false, fmt.Errorf("sync stevedore deployment: %w", err)
	}
	log.Printf("Self-update: synced to %s@%s", result.Branch, result.Commit[:12])

	// Check if update is needed
	selfUpdate := NewSelfUpdate(i, SelfUpdateConfig{})
	needsUpdate, newCommit, err := selfUpdate.NeedsSelfUpdate(ctx, currentCommit)
	if err != nil {
		return false, fmt.Errorf("check for updates: %w", err)
	}

	if !needsUpdate {
		log.Printf("Self-update: already at latest commit %s", currentCommit[:12])
		return false, nil
	}

	log.Printf("Self-update: update available (%s -> %s)", currentCommit[:12], newCommit[:12])

	// Build new image
	newImage, err := selfUpdate.BuildNewImage(ctx)
	if err != nil {
		return false, fmt.Errorf("build new image: %w", err)
	}

	// Execute update (this spawns a worker that will replace our container)
	if err := selfUpdate.Execute(ctx, newImage); err != nil {
		return false, fmt.Errorf("execute self-update: %w", err)
	}

	return true, nil
}
