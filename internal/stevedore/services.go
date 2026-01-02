package stevedore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// Service represents a discovered service with its metadata.
type Service struct {
	// Deployment name (from stevedore)
	Deployment string `json:"deployment"`
	// Service name (from compose)
	ServiceName string `json:"service"`
	// Container ID
	ContainerID string `json:"container_id"`
	// Container name
	ContainerName string `json:"container_name"`
	// Whether the container is running
	Running bool `json:"running"`
	// Ingress configuration (if enabled)
	Ingress *IngressConfig `json:"ingress,omitempty"`
}

// IngressConfig holds ingress-related labels for a service.
type IngressConfig struct {
	// Whether ingress is enabled
	Enabled bool `json:"enabled"`
	// Subdomain for routing
	Subdomain string `json:"subdomain,omitempty"`
	// Port to route to
	Port int `json:"port,omitempty"`
	// Whether WebSocket support is needed
	WebSocket bool `json:"websocket,omitempty"`
	// Health check path
	HealthCheck string `json:"healthcheck,omitempty"`
}

// Label constants for service discovery
const (
	LabelStevedoreDeployment = "com.stevedore.deployment"
	LabelComposeProject      = "com.docker.compose.project"
	LabelComposeService      = "com.docker.compose.service"

	// Ingress labels
	LabelIngressEnabled     = "stevedore.ingress.enabled"
	LabelIngressSubdomain   = "stevedore.ingress.subdomain"
	LabelIngressPort        = "stevedore.ingress.port"
	LabelIngressWebSocket   = "stevedore.ingress.websocket"
	LabelIngressHealthCheck = "stevedore.ingress.healthcheck"
)

// Parameter-based ingress configuration constants (Issue #9)
const (
	ParamIngressPrefix      = "STEVEDORE_INGRESS_"
	ParamIngressEnabled     = "STEVEDORE_INGRESS_ENABLED"
	ParamIngressSubdomain   = "STEVEDORE_INGRESS_SUBDOMAIN"
	ParamIngressPort        = "STEVEDORE_INGRESS_PORT"
	ParamIngressWebSocket   = "STEVEDORE_INGRESS_WEBSOCKET"
	ParamIngressHealthCheck = "STEVEDORE_INGRESS_HEALTHCHECK"
)

// dockerContainerInfo holds minimal container info from docker ps/inspect
type dockerContainerInfo struct {
	ID     string            `json:"Id"`
	Name   string            `json:"Name"`
	State  containerState    `json:"State"`
	Config containerConfig   `json:"Config"`
}

type containerState struct {
	Running bool `json:"Running"`
}

type containerConfig struct {
	Labels map[string]string `json:"Labels"`
}

// ListServices returns all services managed by stevedore.
func (i *Instance) ListServices(ctx context.Context) ([]Service, error) {
	// List all containers that belong to stevedore projects
	ids, err := i.listStevedoreContainerIDs(ctx)
	if err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return nil, nil
	}

	// Cache for deployment params to avoid repeated DB queries
	deploymentParams := make(map[string]map[string]string)

	// Inspect each container
	var services []Service
	for _, id := range ids {
		svc, err := i.inspectServiceWithParams(ctx, id, deploymentParams)
		if err != nil {
			continue // Skip containers we can't inspect
		}
		services = append(services, *svc)
	}

	// Sort by deployment, then service name
	sort.Slice(services, func(i, j int) bool {
		if services[i].Deployment != services[j].Deployment {
			return services[i].Deployment < services[j].Deployment
		}
		return services[i].ServiceName < services[j].ServiceName
	})

	return services, nil
}

// ListIngressServices returns only services with ingress enabled.
func (i *Instance) ListIngressServices(ctx context.Context) ([]Service, error) {
	all, err := i.ListServices(ctx)
	if err != nil {
		return nil, err
	}

	var ingress []Service
	for _, svc := range all {
		if svc.Ingress != nil && svc.Ingress.Enabled {
			ingress = append(ingress, svc)
		}
	}

	return ingress, nil
}

// listStevedoreContainerIDs returns IDs of all containers belonging to stevedore projects.
func (i *Instance) listStevedoreContainerIDs(ctx context.Context) ([]string, error) {
	// Find all containers with project names starting with "stevedore-"
	args := []string{
		"ps", "-a",
		"--filter", "label=" + LabelComposeProject,
		"--format", "{{.ID}}\t{{.Label \"" + LabelComposeProject + "\"}}",
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to list containers: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	var ids []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		id := parts[0]
		project := parts[1]
		// Only include stevedore-managed projects
		if strings.HasPrefix(project, "stevedore-") {
			ids = append(ids, id)
		}
	}

	return ids, nil
}

// inspectService gets service info from a container (without parameter support).
func (i *Instance) inspectService(ctx context.Context, containerID string) (*Service, error) {
	return i.inspectServiceWithParams(ctx, containerID, nil)
}

// inspectServiceWithParams gets service info from a container with parameter-based ingress support.
// The deploymentParams cache is used to avoid repeated DB queries for the same deployment.
func (i *Instance) inspectServiceWithParams(ctx context.Context, containerID string, deploymentParamsCache map[string]map[string]string) (*Service, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", containerID)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker inspect failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var results []dockerContainerInfo
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("failed to parse docker inspect output: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no container found")
	}

	r := results[0]
	labels := r.Config.Labels

	// Extract deployment name from project (stevedore-{deployment})
	project := labels[LabelComposeProject]
	deployment := strings.TrimPrefix(project, "stevedore-")
	serviceName := labels[LabelComposeService]

	svc := &Service{
		Deployment:    deployment,
		ServiceName:   serviceName,
		ContainerID:   r.ID[:12],
		ContainerName: strings.TrimPrefix(r.Name, "/"),
		Running:       r.State.Running,
	}

	// Parse ingress labels first (labels take precedence)
	ingress := parseIngressLabels(labels)

	// If no ingress from labels, try parameters
	if ingress == nil && deploymentParamsCache != nil && deployment != "" && serviceName != "" {
		// Get or load deployment params
		params, ok := deploymentParamsCache[deployment]
		if !ok {
			params, _ = i.LoadDeploymentIngressParams(deployment)
			deploymentParamsCache[deployment] = params
		}

		if len(params) > 0 {
			ingress = parseIngressFromParams(params, serviceName)
		}
	}

	if ingress != nil {
		svc.Ingress = ingress
	}

	return svc, nil
}

// parseIngressLabels extracts ingress configuration from container labels.
func parseIngressLabels(labels map[string]string) *IngressConfig {
	enabledStr := labels[LabelIngressEnabled]
	if enabledStr == "" {
		return nil
	}

	enabled := enabledStr == "true" || enabledStr == "1" || enabledStr == "yes"

	config := &IngressConfig{
		Enabled:     enabled,
		Subdomain:   labels[LabelIngressSubdomain],
		HealthCheck: labels[LabelIngressHealthCheck],
	}

	// Parse port
	if portStr := labels[LabelIngressPort]; portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			config.Port = port
		}
	}

	// Parse websocket
	wsStr := labels[LabelIngressWebSocket]
	config.WebSocket = wsStr == "true" || wsStr == "1" || wsStr == "yes"

	return config
}

// normalizeServiceName converts a service name to uppercase with dashes replaced by underscores.
// This follows the industry standard for environment variable naming.
func normalizeServiceName(serviceName string) string {
	return strings.ToUpper(strings.ReplaceAll(serviceName, "-", "_"))
}

// parseIngressFromParams extracts ingress configuration from deployment parameters.
// Service-specific params (STEVEDORE_INGRESS_<SERVICE>_*) take precedence.
// If no service-specific params exist, returns nil (must be explicit per Issue #9).
func parseIngressFromParams(params map[string]string, serviceName string) *IngressConfig {
	if len(params) == 0 {
		return nil
	}

	// Try service-specific params first: STEVEDORE_INGRESS_<SERVICE>_*
	normalizedService := normalizeServiceName(serviceName)
	servicePrefix := ParamIngressPrefix + normalizedService + "_"

	// Check if service-specific enabled param exists
	enabledKey := servicePrefix + "ENABLED"
	enabledStr, hasServiceSpecific := params[enabledKey]

	if !hasServiceSpecific {
		// No service-specific config - must be explicit (no fallback to deployment-wide)
		return nil
	}

	enabled := enabledStr == "true" || enabledStr == "1" || enabledStr == "yes"

	config := &IngressConfig{
		Enabled:     enabled,
		Subdomain:   params[servicePrefix+"SUBDOMAIN"],
		HealthCheck: params[servicePrefix+"HEALTHCHECK"],
	}

	// Parse port
	if portStr := params[servicePrefix+"PORT"]; portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			config.Port = port
		}
	}

	// Parse websocket
	wsStr := params[servicePrefix+"WEBSOCKET"]
	config.WebSocket = wsStr == "true" || wsStr == "1" || wsStr == "yes"

	return config
}

// LoadDeploymentIngressParams loads ingress-related parameters for a deployment.
func (i *Instance) LoadDeploymentIngressParams(deployment string) (map[string]string, error) {
	params := make(map[string]string)

	names, err := i.ListParameters(deployment)
	if err != nil {
		return params, nil // Return empty map on error (deployment might not exist)
	}

	for _, name := range names {
		if strings.HasPrefix(name, ParamIngressPrefix) {
			value, err := i.GetParameter(deployment, name)
			if err == nil {
				params[name] = string(value)
			}
		}
	}

	return params, nil
}
