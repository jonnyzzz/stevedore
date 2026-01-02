package stevedore

import (
	"testing"
)

func TestParseIngressLabels_Empty(t *testing.T) {
	labels := map[string]string{}
	config := parseIngressLabels(labels)
	if config != nil {
		t.Error("parseIngressLabels() expected nil for empty labels")
	}
}

func TestParseIngressLabels_NotEnabled(t *testing.T) {
	labels := map[string]string{
		"stevedore.ingress.subdomain": "myapp",
		"stevedore.ingress.port":      "8080",
	}
	config := parseIngressLabels(labels)
	if config != nil {
		t.Error("parseIngressLabels() expected nil when enabled is not set")
	}
}

func TestParseIngressLabels_EnabledTrue(t *testing.T) {
	labels := map[string]string{
		LabelIngressEnabled:   "true",
		LabelIngressSubdomain: "myapp",
		LabelIngressPort:      "8080",
	}
	config := parseIngressLabels(labels)
	if config == nil {
		t.Fatal("parseIngressLabels() returned nil")
	}
	if !config.Enabled {
		t.Error("Enabled = false, want true")
	}
	if config.Subdomain != "myapp" {
		t.Errorf("Subdomain = %q, want %q", config.Subdomain, "myapp")
	}
	if config.Port != 8080 {
		t.Errorf("Port = %d, want %d", config.Port, 8080)
	}
}

func TestParseIngressLabels_Enabled1(t *testing.T) {
	labels := map[string]string{
		LabelIngressEnabled: "1",
	}
	config := parseIngressLabels(labels)
	if config == nil {
		t.Fatal("parseIngressLabels() returned nil")
	}
	if !config.Enabled {
		t.Error("Enabled = false, want true (for '1')")
	}
}

func TestParseIngressLabels_EnabledYes(t *testing.T) {
	labels := map[string]string{
		LabelIngressEnabled: "yes",
	}
	config := parseIngressLabels(labels)
	if config == nil {
		t.Fatal("parseIngressLabels() returned nil")
	}
	if !config.Enabled {
		t.Error("Enabled = false, want true (for 'yes')")
	}
}

func TestParseIngressLabels_EnabledFalse(t *testing.T) {
	labels := map[string]string{
		LabelIngressEnabled: "false",
	}
	config := parseIngressLabels(labels)
	if config == nil {
		t.Fatal("parseIngressLabels() returned nil")
	}
	if config.Enabled {
		t.Error("Enabled = true, want false")
	}
}

func TestParseIngressLabels_WebSocket(t *testing.T) {
	labels := map[string]string{
		LabelIngressEnabled:   "true",
		LabelIngressWebSocket: "true",
	}
	config := parseIngressLabels(labels)
	if config == nil {
		t.Fatal("parseIngressLabels() returned nil")
	}
	if !config.WebSocket {
		t.Error("WebSocket = false, want true")
	}
}

func TestParseIngressLabels_HealthCheck(t *testing.T) {
	labels := map[string]string{
		LabelIngressEnabled:     "true",
		LabelIngressHealthCheck: "/health",
	}
	config := parseIngressLabels(labels)
	if config == nil {
		t.Fatal("parseIngressLabels() returned nil")
	}
	if config.HealthCheck != "/health" {
		t.Errorf("HealthCheck = %q, want %q", config.HealthCheck, "/health")
	}
}

func TestParseIngressLabels_InvalidPort(t *testing.T) {
	labels := map[string]string{
		LabelIngressEnabled: "true",
		LabelIngressPort:    "not-a-number",
	}
	config := parseIngressLabels(labels)
	if config == nil {
		t.Fatal("parseIngressLabels() returned nil")
	}
	if config.Port != 0 {
		t.Errorf("Port = %d, want 0 (for invalid port)", config.Port)
	}
}

func TestParseIngressLabels_FullConfig(t *testing.T) {
	labels := map[string]string{
		LabelIngressEnabled:     "true",
		LabelIngressSubdomain:   "api",
		LabelIngressPort:        "3000",
		LabelIngressWebSocket:   "yes",
		LabelIngressHealthCheck: "/api/health",
	}
	config := parseIngressLabels(labels)
	if config == nil {
		t.Fatal("parseIngressLabels() returned nil")
	}
	if !config.Enabled {
		t.Error("Enabled = false, want true")
	}
	if config.Subdomain != "api" {
		t.Errorf("Subdomain = %q, want %q", config.Subdomain, "api")
	}
	if config.Port != 3000 {
		t.Errorf("Port = %d, want %d", config.Port, 3000)
	}
	if !config.WebSocket {
		t.Error("WebSocket = false, want true")
	}
	if config.HealthCheck != "/api/health" {
		t.Errorf("HealthCheck = %q, want %q", config.HealthCheck, "/api/health")
	}
}

func TestLabelConstants(t *testing.T) {
	// Verify label constants are correct
	tests := []struct {
		name     string
		constant string
		want     string
	}{
		{"LabelStevedoreDeployment", LabelStevedoreDeployment, "com.stevedore.deployment"},
		{"LabelComposeProject", LabelComposeProject, "com.docker.compose.project"},
		{"LabelComposeService", LabelComposeService, "com.docker.compose.service"},
		{"LabelIngressEnabled", LabelIngressEnabled, "stevedore.ingress.enabled"},
		{"LabelIngressSubdomain", LabelIngressSubdomain, "stevedore.ingress.subdomain"},
		{"LabelIngressPort", LabelIngressPort, "stevedore.ingress.port"},
		{"LabelIngressWebSocket", LabelIngressWebSocket, "stevedore.ingress.websocket"},
		{"LabelIngressHealthCheck", LabelIngressHealthCheck, "stevedore.ingress.healthcheck"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.constant, tt.want)
			}
		})
	}
}

// Tests for parameter-based ingress configuration (Issue #9)

func TestParseIngressFromParams_Empty(t *testing.T) {
	params := map[string]string{}
	config := parseIngressFromParams(params, "web")
	if config != nil {
		t.Error("parseIngressFromParams() expected nil for empty params")
	}
}

func TestParseIngressFromParams_NotEnabled(t *testing.T) {
	// Service-specific params without ENABLED should return nil
	params := map[string]string{
		"STEVEDORE_INGRESS_WEB_SUBDOMAIN": "myapp",
		"STEVEDORE_INGRESS_WEB_PORT":      "8080",
	}
	config := parseIngressFromParams(params, "web")
	if config != nil {
		t.Error("parseIngressFromParams() expected nil when enabled is not set")
	}
}

func TestParseIngressFromParams_EnabledTrue(t *testing.T) {
	// Service-specific params: STEVEDORE_INGRESS_<SERVICE>_*
	params := map[string]string{
		"STEVEDORE_INGRESS_WEB_ENABLED":   "true",
		"STEVEDORE_INGRESS_WEB_SUBDOMAIN": "myapp",
		"STEVEDORE_INGRESS_WEB_PORT":      "8080",
	}
	config := parseIngressFromParams(params, "web")
	if config == nil {
		t.Fatal("parseIngressFromParams() returned nil")
	}
	if !config.Enabled {
		t.Error("Enabled = false, want true")
	}
	if config.Subdomain != "myapp" {
		t.Errorf("Subdomain = %q, want %q", config.Subdomain, "myapp")
	}
	if config.Port != 8080 {
		t.Errorf("Port = %d, want %d", config.Port, 8080)
	}
}

func TestParseIngressFromParams_Enabled1(t *testing.T) {
	params := map[string]string{
		"STEVEDORE_INGRESS_WEB_ENABLED": "1",
	}
	config := parseIngressFromParams(params, "web")
	if config == nil {
		t.Fatal("parseIngressFromParams() returned nil")
	}
	if !config.Enabled {
		t.Error("Enabled = false, want true (for '1')")
	}
}

func TestParseIngressFromParams_EnabledYes(t *testing.T) {
	params := map[string]string{
		"STEVEDORE_INGRESS_WEB_ENABLED": "yes",
	}
	config := parseIngressFromParams(params, "web")
	if config == nil {
		t.Fatal("parseIngressFromParams() returned nil")
	}
	if !config.Enabled {
		t.Error("Enabled = false, want true (for 'yes')")
	}
}

func TestParseIngressFromParams_EnabledFalse(t *testing.T) {
	params := map[string]string{
		"STEVEDORE_INGRESS_WEB_ENABLED": "false",
	}
	config := parseIngressFromParams(params, "web")
	if config == nil {
		t.Fatal("parseIngressFromParams() returned nil")
	}
	if config.Enabled {
		t.Error("Enabled = true, want false")
	}
}

func TestParseIngressFromParams_WebSocket(t *testing.T) {
	params := map[string]string{
		"STEVEDORE_INGRESS_WEB_ENABLED":   "true",
		"STEVEDORE_INGRESS_WEB_WEBSOCKET": "true",
	}
	config := parseIngressFromParams(params, "web")
	if config == nil {
		t.Fatal("parseIngressFromParams() returned nil")
	}
	if !config.WebSocket {
		t.Error("WebSocket = false, want true")
	}
}

func TestParseIngressFromParams_HealthCheck(t *testing.T) {
	params := map[string]string{
		"STEVEDORE_INGRESS_WEB_ENABLED":     "true",
		"STEVEDORE_INGRESS_WEB_HEALTHCHECK": "/health",
	}
	config := parseIngressFromParams(params, "web")
	if config == nil {
		t.Fatal("parseIngressFromParams() returned nil")
	}
	if config.HealthCheck != "/health" {
		t.Errorf("HealthCheck = %q, want %q", config.HealthCheck, "/health")
	}
}

func TestParseIngressFromParams_FullConfig(t *testing.T) {
	params := map[string]string{
		"STEVEDORE_INGRESS_WEB_ENABLED":     "true",
		"STEVEDORE_INGRESS_WEB_SUBDOMAIN":   "api",
		"STEVEDORE_INGRESS_WEB_PORT":        "3000",
		"STEVEDORE_INGRESS_WEB_WEBSOCKET":   "yes",
		"STEVEDORE_INGRESS_WEB_HEALTHCHECK": "/api/health",
	}
	config := parseIngressFromParams(params, "web")
	if config == nil {
		t.Fatal("parseIngressFromParams() returned nil")
	}
	if !config.Enabled {
		t.Error("Enabled = false, want true")
	}
	if config.Subdomain != "api" {
		t.Errorf("Subdomain = %q, want %q", config.Subdomain, "api")
	}
	if config.Port != 3000 {
		t.Errorf("Port = %d, want %d", config.Port, 3000)
	}
	if !config.WebSocket {
		t.Error("WebSocket = false, want true")
	}
	if config.HealthCheck != "/api/health" {
		t.Errorf("HealthCheck = %q, want %q", config.HealthCheck, "/api/health")
	}
}

func TestParseIngressFromParams_ServiceSpecific(t *testing.T) {
	// Service-specific params should override deployment-wide params
	params := map[string]string{
		"STEVEDORE_INGRESS_ENABLED":         "true",
		"STEVEDORE_INGRESS_SUBDOMAIN":       "default",
		"STEVEDORE_INGRESS_WEB_ENABLED":     "true",
		"STEVEDORE_INGRESS_WEB_SUBDOMAIN":   "web-specific",
		"STEVEDORE_INGRESS_WEB_PORT":        "9000",
		"STEVEDORE_INGRESS_API_ENABLED":     "true",
		"STEVEDORE_INGRESS_API_SUBDOMAIN":   "api-specific",
	}

	// Test web service gets service-specific config
	webConfig := parseIngressFromParams(params, "web")
	if webConfig == nil {
		t.Fatal("parseIngressFromParams(web) returned nil")
	}
	if webConfig.Subdomain != "web-specific" {
		t.Errorf("web Subdomain = %q, want %q", webConfig.Subdomain, "web-specific")
	}
	if webConfig.Port != 9000 {
		t.Errorf("web Port = %d, want %d", webConfig.Port, 9000)
	}

	// Test api service gets service-specific config
	apiConfig := parseIngressFromParams(params, "api")
	if apiConfig == nil {
		t.Fatal("parseIngressFromParams(api) returned nil")
	}
	if apiConfig.Subdomain != "api-specific" {
		t.Errorf("api Subdomain = %q, want %q", apiConfig.Subdomain, "api-specific")
	}

	// Test unknown service gets NO config (must be explicit)
	unknownConfig := parseIngressFromParams(params, "unknown")
	if unknownConfig != nil {
		t.Error("parseIngressFromParams(unknown) should return nil - no explicit config")
	}
}

func TestParseIngressFromParams_ServiceNameWithDashes(t *testing.T) {
	// Service names with dashes should be converted to underscores (industry standard)
	params := map[string]string{
		"STEVEDORE_INGRESS_MY_WEB_SERVICE_ENABLED":   "true",
		"STEVEDORE_INGRESS_MY_WEB_SERVICE_SUBDOMAIN": "my-web",
		"STEVEDORE_INGRESS_MY_WEB_SERVICE_PORT":      "8080",
	}

	config := parseIngressFromParams(params, "my-web-service")
	if config == nil {
		t.Fatal("parseIngressFromParams(my-web-service) returned nil")
	}
	if config.Subdomain != "my-web" {
		t.Errorf("Subdomain = %q, want %q", config.Subdomain, "my-web")
	}
	if config.Port != 8080 {
		t.Errorf("Port = %d, want %d", config.Port, 8080)
	}
}

func TestNormalizeServiceName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"web", "WEB"},
		{"my-web-service", "MY_WEB_SERVICE"},
		{"api_v2", "API_V2"},
		{"MyService", "MYSERVICE"},
		{"service-with--double-dash", "SERVICE_WITH__DOUBLE_DASH"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeServiceName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeServiceName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParamIngressConstants(t *testing.T) {
	// Verify parameter constants are correct
	tests := []struct {
		name     string
		constant string
		want     string
	}{
		{"ParamIngressPrefix", ParamIngressPrefix, "STEVEDORE_INGRESS_"},
		{"ParamIngressEnabled", ParamIngressEnabled, "STEVEDORE_INGRESS_ENABLED"},
		{"ParamIngressSubdomain", ParamIngressSubdomain, "STEVEDORE_INGRESS_SUBDOMAIN"},
		{"ParamIngressPort", ParamIngressPort, "STEVEDORE_INGRESS_PORT"},
		{"ParamIngressWebSocket", ParamIngressWebSocket, "STEVEDORE_INGRESS_WEBSOCKET"},
		{"ParamIngressHealthCheck", ParamIngressHealthCheck, "STEVEDORE_INGRESS_HEALTHCHECK"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.constant, tt.want)
			}
		})
	}
}
