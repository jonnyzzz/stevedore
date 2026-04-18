package stevedore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	// Build controls whether images are rebuilt (--build flag).
	// Set to true for deploy-after-sync (source code changed).
	// Set to false for reconcile restarts (just restart existing images).
	Build bool
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

	// Enforce that every service enables `init: true` (or explicitly opts out
	// via the `stevedore.init.required=false` label). This makes Docker use
	// tini as PID 1 inside each container, which reaps orphans that would
	// otherwise accumulate as zombies and exhaust the cgroup PID limit.
	if err := i.checkInitRequirement(ctx, composePath, projectName, gitDir); err != nil {
		return nil, err
	}

	// Run docker compose up
	args := []string{
		"compose",
		"-f", composePath,
		"-p", projectName,
		"up", "-d",
	}
	if config.Build {
		// --build ensures images are rebuilt when source code changes (deploy after sync)
		args = append(args, "--build")
	}
	args = append(args, "--remove-orphans")

	cmd := newCommand(ctx, "docker", args...)
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

	if err := runCommand(cmd); err != nil {
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

	cmd := newCommand(ctx, "docker", args...)
	if composePath != "" {
		cmd.Dir = gitDir
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := runCommand(cmd); err != nil {
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

	cmd := newCommand(ctx, "docker", args...)
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := runCommand(cmd); err != nil {
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

// InitEnforceLabel names a Compose service label that opts out of the `init: true`
// enforcement performed on every deploy. Default behavior is enforce=true for
// every service; set this label to "false" to skip the check.
//
// Use sparingly: services that opt out must reap their own orphans (e.g. run
// tini, s6-overlay, or another init inside the image). See docs/INIT.md.
const InitEnforceLabel = "stevedore.init.enforce"

// checkInitRequirement resolves the compose project and verifies that every
// service has `init: true` set, or opts out via the InitEnforceLabel label set
// to "false". On failure, returns an error that names the offending services
// and instructs how to fix them.
func (i *Instance) checkInitRequirement(ctx context.Context, composePath, projectName, gitDir string) error {
	services, err := parseComposeServicesJSON(ctx, composePath, projectName, gitDir)
	if err != nil {
		return fmt.Errorf("failed to resolve compose services for init check: %w", err)
	}

	missing := servicesMissingInit(services)
	if len(missing) == 0 {
		return nil
	}

	return fmt.Errorf(
		"init: true is required on every service — missing in: %s\n"+
			"\n"+
			"Fix the compose file by adding `init: true` to each listed service, e.g.:\n"+
			"\n"+
			"  services:\n"+
			"    %s:\n"+
			"      init: true\n"+
			"\n"+
			"This tells Docker to use tini as PID 1 and reap orphaned subprocesses.\n"+
			"If a service manages its own init (e.g. s6-overlay), opt out with the label\n"+
			"  labels:\n"+
			"    %s: \"false\"\n",
		strings.Join(missing, ", "),
		missing[0],
		InitEnforceLabel,
	)
}

// composeConfigService is the subset of `docker compose config --format json`
// output that the init-enforcement check needs.
type composeConfigService struct {
	Init   *bool             `json:"init"`
	Labels map[string]string `json:"labels"`
}

// parseComposeServicesJSON runs `docker compose config --format json` and
// returns a name → service-config map for use by the init check.
func parseComposeServicesJSON(ctx context.Context, composePath, projectName, gitDir string) (map[string]composeConfigService, error) {
	args := []string{
		"compose",
		"-f", composePath,
		"-p", projectName,
		"config", "--format", "json",
	}
	cmd := newCommand(ctx, "docker", args...)
	cmd.Dir = gitDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := runCommand(cmd); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var parsed struct {
		Services map[string]composeConfigService `json:"services"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return nil, fmt.Errorf("parse compose config json: %w", err)
	}
	return parsed.Services, nil
}

// servicesMissingInit returns the sorted names of services that neither enable
// `init: true` nor opt out via the InitEnforceLabel label set to "false".
func servicesMissingInit(services map[string]composeConfigService) []string {
	var missing []string
	for name, svc := range services {
		if svc.Init != nil && *svc.Init {
			continue
		}
		if strings.EqualFold(svc.Labels[InitEnforceLabel], "false") {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	return missing
}
