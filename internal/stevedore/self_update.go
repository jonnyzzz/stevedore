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
		log.Printf("Current commit is unknown, forcing self-update to %s", newCommit[:12])
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
// If imageTag is empty, it uses the current container's image tag so that
// the restart policy will pick up the new image.
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
		// Use the same tag as the current container so restart picks up the new image
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
		// Continue anyway - backup is nice to have but not critical
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

// Execute performs the self-update by exiting the current container.
// The restart policy (systemd or Docker) will restart the container with the new image.
//
// NOTE: This method will cause the current process to exit!
func (s *SelfUpdate) Execute(ctx context.Context, newImageTag string) error {
	log.Printf("Self-update: new image %s is ready", newImageTag)
	log.Printf("Self-update: exiting container to trigger restart with new image")
	log.Printf("Self-update: the restart policy (systemd or Docker) will restart the container")

	// Signal that we're doing a planned self-update exit
	// This could be used by monitoring to distinguish from crashes
	os.Exit(0)

	return nil // Never reached
}

// IsStevedoreDeployment returns true if the deployment is the stevedore self-deployment.
func IsStevedoreDeployment(name string) bool {
	return name == "stevedore"
}

// TriggerSelfUpdate performs a self-update if there are changes available.
// It syncs the stevedore deployment, builds a new image (with the same tag as current),
// and exits to let the restart policy bring up the new version.
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

	// Build new image (this uses the same tag as current, so restart picks up new image)
	newImage, err := selfUpdate.BuildNewImage(ctx)
	if err != nil {
		return false, fmt.Errorf("build new image: %w", err)
	}

	// Execute update (this exits the container - restart policy will restart with new image)
	if err := selfUpdate.Execute(ctx, newImage); err != nil {
		return false, fmt.Errorf("execute self-update: %w", err)
	}

	return true, nil
}
