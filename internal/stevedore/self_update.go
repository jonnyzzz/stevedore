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
		// Can't determine current version, assume no update needed
		return false, newCommit, nil
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

	// Get the current container's configuration
	inspectCmd := exec.CommandContext(ctx, "docker", "inspect", "--format",
		"{{range .Mounts}}{{.Source}}:{{.Destination}}:{{if .RW}}rw{{else}}ro{{end}} {{end}}",
		containerName)
	var mountsOut bytes.Buffer
	inspectCmd.Stdout = &mountsOut
	if err := inspectCmd.Run(); err != nil {
		return fmt.Errorf("inspect container mounts: %w", err)
	}

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

	// Create the update script that will run in a separate container
	updateScript := fmt.Sprintf(`#!/bin/sh
set -e

echo "Self-update worker starting..."

# Wait a moment for the original container to be ready for replacement
sleep 2

# Stop the current container
echo "Stopping container: %s"
docker stop %s || true

# Remove the old container
echo "Removing old container..."
docker rm %s || true

# Start new container with the same configuration
echo "Starting new container with image: %s"
docker run -d \
  --name %s \
  --restart %s \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /opt/stevedore:/opt/stevedore \
  %s \
  /app/stevedore -d

echo "Self-update complete!"
`, containerName, containerName, containerName, newImageTag, containerName, restartPolicy, newImageTag)

	// Run the update worker
	workerName := fmt.Sprintf("stevedore-update-%d", time.Now().Unix())

	log.Printf("Spawning update worker: %s", workerName)

	// Write script to temp file (the worker needs to access it)
	scriptPath := filepath.Join(s.instance.SystemDir(), "update-script.sh")
	if err := os.WriteFile(scriptPath, []byte(updateScript), 0755); err != nil {
		return fmt.Errorf("write update script: %w", err)
	}

	// Run the update worker container
	args := []string{
		"run", "-d",
		"--name", workerName,
		"--rm",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", s.instance.SystemDir() + ":/stevedore-system:ro",
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
