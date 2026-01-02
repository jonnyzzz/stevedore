package stevedore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ContainerHealth represents the health status of a container.
type ContainerHealth string

const (
	HealthHealthy   ContainerHealth = "healthy"
	HealthUnhealthy ContainerHealth = "unhealthy"
	HealthStarting  ContainerHealth = "starting"
	HealthNone      ContainerHealth = "none" // No health check configured
)

// IsHealthy returns true if the container health indicates a healthy state.
func (h ContainerHealth) IsHealthy() bool {
	return h == HealthHealthy || h == HealthNone || h == HealthStarting
}

// ContainerState represents the state of a container.
type ContainerState string

const (
	StateRunning    ContainerState = "running"
	StateExited     ContainerState = "exited"
	StateCreated    ContainerState = "created"
	StateRestarting ContainerState = "restarting"
	StatePaused     ContainerState = "paused"
	StateDead       ContainerState = "dead"
)

// IsRunning returns true if the container is in a running or starting state.
func (s ContainerState) IsRunning() bool {
	return s == StateRunning || s == StateRestarting || s == StateCreated
}

// IsStopped returns true if the container is in a stopped state.
func (s ContainerState) IsStopped() bool {
	return s == StateExited || s == StatePaused || s == StateDead
}

// ContainerStatus holds status information for a container.
type ContainerStatus struct {
	// Container ID
	ID string `json:"id"`
	// Container name
	Name string `json:"name"`
	// Service name (from compose)
	Service string `json:"service"`
	// Image used
	Image string `json:"image"`
	// Current state
	State ContainerState `json:"state"`
	// Health status (if health check is configured)
	Health ContainerHealth `json:"health"`
	// Status string (e.g., "Up 2 hours")
	Status string `json:"status"`
	// Exit code (if exited)
	ExitCode int `json:"exit_code"`
	// Started at timestamp
	StartedAt time.Time `json:"started_at"`
}

// DeploymentStatus holds the overall status of a deployment.
type DeploymentStatus struct {
	// Deployment name
	Deployment string `json:"deployment"`
	// Project name
	ProjectName string `json:"project_name"`
	// List of containers
	Containers []ContainerStatus `json:"containers"`
	// Overall health (healthy if all containers are healthy/running)
	Healthy bool `json:"healthy"`
	// Status message
	Message string `json:"message"`
}

// dockerInspectResult matches the JSON output of docker inspect.
type dockerInspectResult struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	State struct {
		Status    string `json:"Status"`
		Running   bool   `json:"Running"`
		ExitCode  int    `json:"ExitCode"`
		StartedAt string `json:"StartedAt"`
		Health    *struct {
			Status string `json:"Status"`
		} `json:"Health,omitempty"`
	} `json:"State"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

// GetDeploymentStatus returns the current status of a deployment.
func (i *Instance) GetDeploymentStatus(ctx context.Context, deployment string) (*DeploymentStatus, error) {
	if err := ValidateDeploymentName(deployment); err != nil {
		return nil, err
	}

	projectName := ComposeProjectName(deployment)

	// List containers for this project
	containers, err := i.listProjectContainers(ctx, projectName)
	if err != nil {
		return nil, err
	}

	status := &DeploymentStatus{
		Deployment:  deployment,
		ProjectName: projectName,
		Containers:  containers,
		Healthy:     true,
	}

	if len(containers) == 0 {
		status.Healthy = false
		status.Message = "No containers found"
		return status, nil
	}

	// Check overall health
	runningCount := 0
	for _, c := range containers {
		if c.State == StateRunning {
			runningCount++
			if c.Health == HealthUnhealthy {
				status.Healthy = false
			}
		} else {
			status.Healthy = false
		}
	}

	if status.Healthy {
		status.Message = fmt.Sprintf("All %d containers healthy", len(containers))
	} else {
		status.Message = fmt.Sprintf("%d/%d containers running", runningCount, len(containers))
	}

	return status, nil
}

// listProjectContainers lists all containers for a compose project.
func (i *Instance) listProjectContainers(ctx context.Context, projectName string) ([]ContainerStatus, error) {
	// Use docker ps with filter for compose project
	args := []string{
		"ps", "-a",
		"--filter", "label=com.docker.compose.project=" + projectName,
		"--format", "{{.ID}}",
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to list containers: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	ids := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(ids) == 0 || (len(ids) == 1 && ids[0] == "") {
		return nil, nil
	}

	// Get detailed info for each container
	var containers []ContainerStatus
	for _, id := range ids {
		if id == "" {
			continue
		}
		status, err := i.inspectContainer(ctx, id)
		if err != nil {
			continue // Skip containers we can't inspect
		}
		containers = append(containers, *status)
	}

	return containers, nil
}

// inspectContainer gets detailed status for a container.
func (i *Instance) inspectContainer(ctx context.Context, containerID string) (*ContainerStatus, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", containerID)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker inspect failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var results []dockerInspectResult
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("failed to parse docker inspect output: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no container found")
	}

	r := results[0]
	status := &ContainerStatus{
		ID:       r.ID[:12], // Short ID
		Name:     strings.TrimPrefix(r.Name, "/"),
		Image:    r.Config.Image,
		State:    ContainerState(r.State.Status),
		ExitCode: r.State.ExitCode,
	}

	// Extract service name from labels
	if service, ok := r.Config.Labels["com.docker.compose.service"]; ok {
		status.Service = service
	}

	// Parse health status
	if r.State.Health != nil {
		status.Health = ContainerHealth(r.State.Health.Status)
	} else {
		status.Health = HealthNone
	}

	// Parse started at time
	if t, err := time.Parse(time.RFC3339Nano, r.State.StartedAt); err == nil {
		status.StartedAt = t
	}

	// Generate status string
	if r.State.Running {
		uptime := time.Since(status.StartedAt).Truncate(time.Second)
		status.Status = fmt.Sprintf("Up %s", formatDuration(uptime))
		if status.Health != HealthNone {
			status.Status += fmt.Sprintf(" (%s)", status.Health)
		}
	} else {
		status.Status = fmt.Sprintf("Exited (%d)", r.State.ExitCode)
	}

	return status, nil
}

// WaitForHealthy waits for all containers in a deployment to be healthy.
func (i *Instance) WaitForHealthy(ctx context.Context, deployment string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for deployment to be healthy")
		case <-ticker.C:
			status, err := i.GetDeploymentStatus(ctx, deployment)
			if err != nil {
				continue
			}
			if status.Healthy && len(status.Containers) > 0 {
				return nil
			}
		}
	}
}

// formatDuration formats a duration in a human-readable way.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf("%dd", days)
}
