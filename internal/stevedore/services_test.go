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
