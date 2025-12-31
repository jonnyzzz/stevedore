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
	ImageTag      string        // Tag for the new image (if empty, uses current container's image)
	BuildTimeout  time.Duration // Timeout for image build (default: 15m)
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
		log.Printf("Current commit is unknown, forcing self-update to %s", shortCommit(newCommit))
		return true, newCommit, nil
	}

	needsUpdate := newCommit != currentCommit
	return needsUpdate, newCommit, nil
}

// getCurrentImageTag gets the image tag of the currently running stevedore container.
func (s *SelfUpdate) getCurrentImageTag(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Config.Image}}", s.config.ContainerName)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("inspect container image: %w", err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// tagImageAsBackup tags the current image with a backup tag for rollback.
func (s *SelfUpdate) tagImageAsBackup(ctx context.Context, currentImage string) (string, error) {
	// Parse the image name to create a backup tag
	parts := strings.Split(currentImage, ":")
	baseName := parts[0]
	backupTag := fmt.Sprintf("%s:backup-%d", baseName, time.Now().Unix())

	cmd := exec.CommandContext(ctx, "docker", "tag", currentImage, backupTag)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tag backup image: %w", err)
	}

	log.Printf("Tagged current image as backup: %s", backupTag)
	return backupTag, nil
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

	// Determine the image tag to use
	imageTag := s.config.ImageTag
	if imageTag == "" {
		// Use the same tag as the current container
		var err error
		imageTag, err = s.getCurrentImageTag(ctx)
		if err != nil {
			return "", fmt.Errorf("get current image tag: %w", err)
		}
		if imageTag == "" {
			imageTag = "stevedore:latest"
		}
	}

	// Tag the current image as backup before overwriting
	backupTag, err := s.tagImageAsBackup(ctx, imageTag)
	if err != nil {
		log.Printf("Warning: could not create backup tag: %v", err)
	} else {
		log.Printf("Backup image available for rollback: %s", backupTag)
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
// The worker will stop the current container, remove it, and start a new one with the new image.
//
// NOTE: This method will cause the current process to exit (the container will be stopped)!
func (s *SelfUpdate) Execute(ctx context.Context, newImageTag string) error {
	containerName := s.config.ContainerName

	log.Printf("Self-update: preparing to replace container %s with image %s", containerName, newImageTag)

	// Get the current container's mount for /opt/stevedore (HOST path)
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

	// Host paths (for docker run command which runs on the host)
	hostSystemDir := hostRoot + "/system"

	// Ensure the container env file is present and has entries before stopping anything.
	envPath := filepath.Join(s.instance.SystemDir(), "container.env")
	envData, err := os.ReadFile(envPath)
	if err != nil {
		return fmt.Errorf("read container env: %w", err)
	}
	envCount := 0
	for _, line := range strings.Split(string(envData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "=") {
			envCount++
		}
	}
	if envCount == 0 {
		return fmt.Errorf("container env is empty: %s", envPath)
	}
	log.Printf("Self-update: loaded %d env entries from %s", envCount, envPath)

	// Create the update script
	// IMPORTANT: This script runs inside the worker container, which mounts:
	//   hostSystemDir -> /worker-data (read-write)
	// The script should use /worker-data for files it needs to access,
	// but use host paths for the docker run command.
	// NOTE: We read the env file inside the worker and pass individual -e flags
	// instead of using --env-file, to avoid host path resolution issues.
	updateScript := fmt.Sprintf(`#!/bin/sh
LOG_FILE="/worker-data/update.log"

log() {
  echo "$@"
  echo "$(date '+%%Y-%%m-%%d %%H:%%M:%%S') $@" >> "$LOG_FILE" 2>/dev/null || true
}

log "Update worker starting..."
log "Container: %s"
log "New image: %s"
log "Host root: %s"
log "Restart policy: %s"

# Verify env file exists before stopping the container
if [ ! -f "/worker-data/container.env" ]; then
  log "ERROR: container.env not found in /worker-data"
  log "Contents of /worker-data:"
  ls -la /worker-data >> "$LOG_FILE" 2>&1
  exit 1
fi

# Read env file and build -e flags
# This avoids host path resolution issues with --env-file
ENV_ARGS=""
ENV_COUNT=0
while IFS= read -r line || [ -n "$line" ]; do
  # Skip empty lines and comments
  case "$line" in
    ""|\#*) continue ;;
  esac
  ENV_ARGS="$ENV_ARGS -e $line"
  ENV_COUNT=$((ENV_COUNT + 1))
done < /worker-data/container.env

if [ "$ENV_COUNT" -eq 0 ]; then
  log "ERROR: container.env is empty"
  exit 1
fi
log "Environment variables loaded: $ENV_COUNT entries"

# Wait for main container to be ready for replacement
sleep 2

# Stop the current container
log "Stopping container %s..."
if docker stop "%s" 2>> "$LOG_FILE"; then
  log "Container stopped"
else
  log "Warning: stop failed (may already be stopped)"
fi

# Remove the container
log "Removing container..."
if docker rm "%s" 2>> "$LOG_FILE"; then
  log "Container removed"
else
  log "Warning: rm failed (may already be removed)"
fi

# Start new container
log "Starting new container with image %s..."
if docker run -d \
  --name "%s" \
  --restart "%s" \
  $ENV_ARGS \
  -p 42107:42107 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "%s:/opt/stevedore" \
  "%s" \
  /app/stevedore -d 2>> "$LOG_FILE"; then
  log "New container started successfully"
else
  log "ERROR: Failed to start new container"
  exit 1
fi

log "Update complete!"
`,
		containerName, newImageTag, hostRoot, restartPolicy,
		containerName, containerName,
		containerName,
		newImageTag, containerName, restartPolicy, hostRoot, newImageTag)

	// Write the update script to our system directory
	// The worker will mount this directory and read the script
	scriptPath := filepath.Join(s.instance.SystemDir(), "update-script.sh")
	if err := os.WriteFile(scriptPath, []byte(updateScript), 0755); err != nil {
		return fmt.Errorf("write update script: %w", err)
	}

	// Run the update worker container
	workerName := fmt.Sprintf("stevedore-update-%d", time.Now().Unix())
	log.Printf("Spawning update worker: %s", workerName)

	// Worker mounts:
	// - Docker socket for docker commands
	// - Host system directory (mapped to /worker-data) for script and env file
	args := []string{
		"run", "-d",
		"--name", workerName,
		"--rm",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", hostSystemDir + ":/worker-data:rw",
		"--label", "com.stevedore.managed=true",
		"--label", "com.stevedore.role=update-worker",
		"docker:cli",
		"sh", "-c", "sh /worker-data/update-script.sh",
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
	log.Printf("Self-update: synced to %s@%s", result.Branch, shortCommit(result.Commit))

	// Check if update is needed
	selfUpdate := NewSelfUpdate(i, SelfUpdateConfig{})
	needsUpdate, newCommit, err := selfUpdate.NeedsSelfUpdate(ctx, currentCommit)
	if err != nil {
		return false, fmt.Errorf("check for updates: %w", err)
	}

	if !needsUpdate {
		log.Printf("Self-update: already at latest commit %s", shortCommit(currentCommit))
		return false, nil
	}

	log.Printf("Self-update: update available (%s -> %s)", shortCommit(currentCommit), shortCommit(newCommit))

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
