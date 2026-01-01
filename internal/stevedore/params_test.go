package stevedore

import (
	"os"
	"path/filepath"
	"testing"
)

// setupDeployment creates a minimal deployment directory for testing.
func setupDeployment(t *testing.T, instance *Instance, deployment string) {
	t.Helper()
	deploymentDir := instance.DeploymentDir(deployment)
	if err := os.MkdirAll(deploymentDir, 0o755); err != nil {
		t.Fatalf("failed to create deployment dir: %v", err)
	}
}

func TestSetParameter(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	setupDeployment(t, instance, "testapp")

	// Set a parameter
	err := instance.SetParameter("testapp", "MY_SECRET", []byte("secret-value"))
	if err != nil {
		t.Fatalf("SetParameter: %v", err)
	}

	// Verify it was stored
	value, err := instance.GetParameter("testapp", "MY_SECRET")
	if err != nil {
		t.Fatalf("GetParameter: %v", err)
	}
	if string(value) != "secret-value" {
		t.Errorf("GetParameter = %q, want %q", string(value), "secret-value")
	}
}

func TestSetParameter_Update(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	setupDeployment(t, instance, "testapp")

	// Set initial value
	if err := instance.SetParameter("testapp", "MY_PARAM", []byte("initial")); err != nil {
		t.Fatalf("SetParameter initial: %v", err)
	}

	// Update value
	if err := instance.SetParameter("testapp", "MY_PARAM", []byte("updated")); err != nil {
		t.Fatalf("SetParameter update: %v", err)
	}

	// Verify updated value
	value, err := instance.GetParameter("testapp", "MY_PARAM")
	if err != nil {
		t.Fatalf("GetParameter: %v", err)
	}
	if string(value) != "updated" {
		t.Errorf("GetParameter = %q, want %q", string(value), "updated")
	}
}

func TestGetParameter_NotFound(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	setupDeployment(t, instance, "testapp")

	_, err := instance.GetParameter("testapp", "NONEXISTENT")
	if err == nil {
		t.Error("GetParameter expected error for nonexistent parameter")
	}
}

func TestGetParameter_DeploymentNotFound(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	_, err := instance.GetParameter("nonexistent", "MY_PARAM")
	if err == nil {
		t.Error("GetParameter expected error for nonexistent deployment")
	}
}

func TestListParameters(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	setupDeployment(t, instance, "testapp")

	// Set multiple parameters
	params := map[string]string{
		"PARAM_A": "value-a",
		"PARAM_B": "value-b",
		"PARAM_C": "value-c",
	}
	for name, value := range params {
		if err := instance.SetParameter("testapp", name, []byte(value)); err != nil {
			t.Fatalf("SetParameter %s: %v", name, err)
		}
	}

	// List parameters
	names, err := instance.ListParameters("testapp")
	if err != nil {
		t.Fatalf("ListParameters: %v", err)
	}

	if len(names) != 3 {
		t.Errorf("ListParameters returned %d params, want 3", len(names))
	}

	// Should be sorted alphabetically
	expected := []string{"PARAM_A", "PARAM_B", "PARAM_C"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("ListParameters[%d] = %q, want %q", i, name, expected[i])
		}
	}
}

func TestListParameters_Empty(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	setupDeployment(t, instance, "testapp")

	names, err := instance.ListParameters("testapp")
	if err != nil {
		t.Fatalf("ListParameters: %v", err)
	}

	if len(names) != 0 {
		t.Errorf("ListParameters = %v, want empty", names)
	}
}

func TestSetParameter_InvalidDeploymentName(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	err := instance.SetParameter("-invalid", "MY_PARAM", []byte("value"))
	if err == nil {
		t.Error("SetParameter expected error for invalid deployment name")
	}
}

func TestSetParameter_InvalidParameterName(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	setupDeployment(t, instance, "testapp")

	err := instance.SetParameter("testapp", "-invalid", []byte("value"))
	if err == nil {
		t.Error("SetParameter expected error for invalid parameter name")
	}
}

func TestParameters_IsolatedByDeployment(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	setupDeployment(t, instance, "app1")
	setupDeployment(t, instance, "app2")

	// Set same param name in different deployments
	if err := instance.SetParameter("app1", "MY_PARAM", []byte("app1-value")); err != nil {
		t.Fatalf("SetParameter app1: %v", err)
	}
	if err := instance.SetParameter("app2", "MY_PARAM", []byte("app2-value")); err != nil {
		t.Fatalf("SetParameter app2: %v", err)
	}

	// Verify isolation
	val1, err := instance.GetParameter("app1", "MY_PARAM")
	if err != nil {
		t.Fatalf("GetParameter app1: %v", err)
	}
	if string(val1) != "app1-value" {
		t.Errorf("app1 MY_PARAM = %q, want %q", string(val1), "app1-value")
	}

	val2, err := instance.GetParameter("app2", "MY_PARAM")
	if err != nil {
		t.Fatalf("GetParameter app2: %v", err)
	}
	if string(val2) != "app2-value" {
		t.Errorf("app2 MY_PARAM = %q, want %q", string(val2), "app2-value")
	}
}

func TestParameters_BinaryValues(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	setupDeployment(t, instance, "testapp")

	// Store binary data
	binaryData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}
	if err := instance.SetParameter("testapp", "BINARY_DATA", binaryData); err != nil {
		t.Fatalf("SetParameter: %v", err)
	}

	// Retrieve and verify
	value, err := instance.GetParameter("testapp", "BINARY_DATA")
	if err != nil {
		t.Fatalf("GetParameter: %v", err)
	}

	if len(value) != len(binaryData) {
		t.Fatalf("binary data length = %d, want %d", len(value), len(binaryData))
	}
	for i, b := range value {
		if b != binaryData[i] {
			t.Errorf("byte[%d] = %02x, want %02x", i, b, binaryData[i])
		}
	}
}

// TestParameters_UsedInDeploy verifies that ListParameters and GetParameter
// work correctly when called from Deploy() to pass env vars to docker-compose.
// This is a regression test for Issue #4.
func TestParameters_UsedInDeploy(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	// Create deployment with repo structure
	deployment := "myapp"
	deploymentDir := instance.DeploymentDir(deployment)
	repoDir := filepath.Join(deploymentDir, "repo", "git")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	// Set parameters that should be passed to compose
	params := map[string]string{
		"DATABASE_URL": "postgres://localhost/myapp",
		"API_KEY":      "secret-api-key",
		"DEBUG":        "true",
	}
	for name, value := range params {
		if err := instance.SetParameter(deployment, name, []byte(value)); err != nil {
			t.Fatalf("SetParameter %s: %v", name, err)
		}
	}

	// Verify we can list and get all parameters (as Deploy() does)
	names, err := instance.ListParameters(deployment)
	if err != nil {
		t.Fatalf("ListParameters: %v", err)
	}

	if len(names) != len(params) {
		t.Errorf("ListParameters returned %d, want %d", len(names), len(params))
	}

	// Verify each parameter can be retrieved (as Deploy() does)
	for _, name := range names {
		value, err := instance.GetParameter(deployment, name)
		if err != nil {
			t.Errorf("GetParameter %s: %v", name, err)
			continue
		}
		expected := params[name]
		if string(value) != expected {
			t.Errorf("GetParameter %s = %q, want %q", name, string(value), expected)
		}
	}
}
