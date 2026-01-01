package stevedore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var composeEntrypointCandidates = []string{
	"docker-compose.yaml",
	"docker-compose.yml",
	"compose.yaml",
	"compose.yml",
	"stevedore.yaml",
}

// FindComposeEntrypoint searches for a compose file in the given directory.
// Returns the full path to the compose file, or an error if not found.
func FindComposeEntrypoint(repoRoot string) (string, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return "", errors.New("repoRoot is required")
	}

	for _, name := range composeEntrypointCandidates {
		path := filepath.Join(repoRoot, name)
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() {
				continue
			}
			return path, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}

	return "", fmt.Errorf("no compose entrypoint found (expected one of: %s)", strings.Join(composeEntrypointCandidates, ", "))
}

// ComposeConfig holds configuration for Compose operations.
type ComposeConfig struct {
	// Timeout for compose operations
	Timeout time.Duration
}

// DefaultComposeConfig returns the default configuration for Compose.
func DefaultComposeConfig() ComposeConfig {
	return ComposeConfig{
		Timeout: 10 * time.Minute,
	}
}

// DeployResult holds the result of a deployment operation.
type DeployResult struct {
	// ComposeFile is the compose file used (basename)
	ComposeFile string
	// ProjectName is the compose project name
	ProjectName string
	// Services is the list of services defined
	Services []string
}

// Deploy runs docker compose up for a deployment.
func (i *Instance) Deploy(ctx context.Context, deployment string, config ComposeConfig) (*DeployResult, error) {
	if err := ValidateDeploymentName(deployment); err != nil {
		return nil, err
	}

	deploymentDir := i.DeploymentDir(deployment)
	gitDir := filepath.Join(deploymentDir, "repo", "git")

	// Check if repo exists
	if _, err := os.Stat(gitDir); err != nil {
		return nil, fmt.Errorf("repository not checked out: %w", err)
	}

	// Find compose file
	composePath, err := FindComposeEntrypoint(gitDir)
	if err != nil {
		return nil, err
	}

	if config.Timeout == 0 {
		config.Timeout = DefaultComposeConfig().Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	projectName := ComposeProjectName(deployment)

	// Ensure data, logs, and shared directories exist
	dataDir := filepath.Join(deploymentDir, "data")
	logsDir := filepath.Join(deploymentDir, "logs")
	sharedDir := filepath.Join(i.Root, "shared")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create shared directory: %w", err)
	}

	// Run docker compose up
	// --build ensures images are rebuilt when Dockerfile changes
	args := []string{
		"compose",
		"-f", composePath,
		"-p", projectName,
		"up", "-d",
		"--build",
		"--remove-orphans",
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = gitDir
	cmd.Env = append(os.Environ(),
		"STEVEDORE_DEPLOYMENT="+deployment,
		"STEVEDORE_DATA="+dataDir,
		"STEVEDORE_LOGS="+logsDir,
		"STEVEDORE_SHARED="+sharedDir,
	)

	// Add parameters from database as environment variables
	paramNames, _ := i.ListParameters(deployment)
	for _, name := range paramNames {
		value, err := i.GetParameter(deployment, name)
		if err == nil {
			cmd.Env = append(cmd.Env, name+"="+string(value))
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker compose up failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	// Get list of services
	services, err := i.getComposeServices(ctx, composePath, projectName, gitDir)
	if err != nil {
		// Non-fatal - we deployed successfully
		services = nil
	}

	return &DeployResult{
		ComposeFile: filepath.Base(composePath),
		ProjectName: projectName,
		Services:    services,
	}, nil
}

// Stop stops all containers for a deployment.
func (i *Instance) Stop(ctx context.Context, deployment string, config ComposeConfig) error {
	if err := ValidateDeploymentName(deployment); err != nil {
		return err
	}

	deploymentDir := i.DeploymentDir(deployment)
	gitDir := filepath.Join(deploymentDir, "repo", "git")

	if config.Timeout == 0 {
		config.Timeout = DefaultComposeConfig().Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	projectName := ComposeProjectName(deployment)

	// Try to find compose file for cleaner shutdown
	composePath, _ := FindComposeEntrypoint(gitDir)

	var args []string
	if composePath != "" {
		args = []string{
			"compose",
			"-f", composePath,
			"-p", projectName,
			"down",
			"--remove-orphans",
		}
	} else {
		// Fallback: stop by project name only
		args = []string{
			"compose",
			"-p", projectName,
			"down",
			"--remove-orphans",
		}
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	if composePath != "" {
		cmd.Dir = gitDir
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose down failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return nil
}

// getComposeServices returns the list of services in a compose file.
func (i *Instance) getComposeServices(ctx context.Context, composePath, projectName, workDir string) ([]string, error) {
	args := []string{
		"compose",
		"-f", composePath,
		"-p", projectName,
		"config", "--services",
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to list services: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	var services []string
	for _, line := range lines {
		if s := strings.TrimSpace(line); s != "" {
			services = append(services, s)
		}
	}
	return services, nil
}

// ComposeProjectName generates the compose project name for a deployment.
func ComposeProjectName(deployment string) string {
	return "stevedore-" + deployment
}
