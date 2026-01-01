package stevedore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateNamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		wantErr   bool
	}{
		{"valid simple", "dyndns", false},
		{"valid with dash", "dyndns-mappings", false},
		{"valid with underscore", "my_config", false},
		{"valid with dot", "config.v1", false},
		{"valid with number", "config1", false},
		{"invalid starts with dash", "-invalid", true},
		{"invalid starts with dot", ".invalid", true},
		{"invalid empty", "", true},
		{"invalid slash", "foo/bar", true},
		{"invalid space", "foo bar", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateNamespace(%q) error = %v, wantErr %v", tt.namespace, err, tt.wantErr)
			}
		})
	}
}

func TestSharedDir(t *testing.T) {
	instance := NewInstance("/opt/stevedore")
	want := "/opt/stevedore/shared"
	got := instance.SharedDir()
	if got != want {
		t.Errorf("SharedDir() = %q, want %q", got, want)
	}
}

func TestListSharedNamespaces_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	instance := NewInstance(tmpDir)

	namespaces, err := instance.ListSharedNamespaces()
	if err != nil {
		t.Fatalf("ListSharedNamespaces() error = %v", err)
	}
	if len(namespaces) != 0 {
		t.Errorf("ListSharedNamespaces() = %v, want empty", namespaces)
	}
}

func TestListSharedNamespaces_WithFiles(t *testing.T) {
	tmpDir := t.TempDir()
	instance := NewInstance(tmpDir)

	// Create shared directory with some files
	sharedDir := filepath.Join(tmpDir, "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create namespace files
	for _, ns := range []string{"alpha", "beta", "gamma"} {
		path := filepath.Join(sharedDir, ns+".yaml")
		if err := os.WriteFile(path, []byte("key: value\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create a non-yaml file (should be ignored)
	if err := os.WriteFile(filepath.Join(sharedDir, "ignored.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}

	namespaces, err := instance.ListSharedNamespaces()
	if err != nil {
		t.Fatalf("ListSharedNamespaces() error = %v", err)
	}

	want := []string{"alpha", "beta", "gamma"}
	if len(namespaces) != len(want) {
		t.Errorf("ListSharedNamespaces() = %v, want %v", namespaces, want)
	}
	for i, ns := range namespaces {
		if ns != want[i] {
			t.Errorf("ListSharedNamespaces()[%d] = %q, want %q", i, ns, want[i])
		}
	}
}

func TestWriteShared_CreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	instance := NewInstance(tmpDir)

	err := instance.WriteShared("test-ns", "mykey", "myvalue")
	if err != nil {
		t.Fatalf("WriteShared() error = %v", err)
	}

	// Verify file exists
	path := filepath.Join(tmpDir, "shared", "test-ns.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("WriteShared() did not create file: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "mykey: myvalue\n" {
		t.Errorf("WriteShared() wrote %q, want %q", string(data), "mykey: myvalue\n")
	}
}

func TestWriteShared_UpdatesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	instance := NewInstance(tmpDir)

	// Write initial value
	if err := instance.WriteShared("test-ns", "key1", "value1"); err != nil {
		t.Fatal(err)
	}

	// Write another key
	if err := instance.WriteShared("test-ns", "key2", "value2"); err != nil {
		t.Fatal(err)
	}

	// Verify both keys exist
	data, err := instance.ReadShared("test-ns")
	if err != nil {
		t.Fatal(err)
	}

	if data["key1"] != "value1" {
		t.Errorf("ReadShared()[key1] = %v, want %q", data["key1"], "value1")
	}
	if data["key2"] != "value2" {
		t.Errorf("ReadShared()[key2] = %v, want %q", data["key2"], "value2")
	}
}

func TestWriteShared_OverwritesKey(t *testing.T) {
	tmpDir := t.TempDir()
	instance := NewInstance(tmpDir)

	// Write initial value
	if err := instance.WriteShared("test-ns", "mykey", "initial"); err != nil {
		t.Fatal(err)
	}

	// Overwrite
	if err := instance.WriteShared("test-ns", "mykey", "updated"); err != nil {
		t.Fatal(err)
	}

	// Verify
	value, err := instance.ReadSharedKey("test-ns", "mykey")
	if err != nil {
		t.Fatal(err)
	}
	if value != "updated" {
		t.Errorf("ReadSharedKey() = %v, want %q", value, "updated")
	}
}

func TestReadShared_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	instance := NewInstance(tmpDir)

	_, err := instance.ReadShared("nonexistent")
	if err == nil {
		t.Error("ReadShared() expected error for nonexistent namespace")
	}
}

func TestReadSharedKey_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	instance := NewInstance(tmpDir)

	// Create namespace
	if err := instance.WriteShared("test-ns", "existing", "value"); err != nil {
		t.Fatal(err)
	}

	// Try to read non-existent key
	_, err := instance.ReadSharedKey("test-ns", "nonexistent")
	if err == nil {
		t.Error("ReadSharedKey() expected error for nonexistent key")
	}
}

func TestWriteShared_ComplexValues(t *testing.T) {
	tmpDir := t.TempDir()
	instance := NewInstance(tmpDir)

	// Write a map value
	mapValue := map[string]interface{}{
		"subdomain": "myapp",
		"port":      8080,
	}
	if err := instance.WriteShared("config", "service", mapValue); err != nil {
		t.Fatal(err)
	}

	// Read it back
	value, err := instance.ReadSharedKey("config", "service")
	if err != nil {
		t.Fatal(err)
	}

	// Verify structure (YAML unmarshals to map[string]interface{})
	m, ok := value.(map[string]interface{})
	if !ok {
		t.Fatalf("ReadSharedKey() returned %T, want map[string]interface{}", value)
	}
	if m["subdomain"] != "myapp" {
		t.Errorf("service.subdomain = %v, want %q", m["subdomain"], "myapp")
	}
	if m["port"] != 8080 {
		t.Errorf("service.port = %v, want %d", m["port"], 8080)
	}
}

func TestReadSharedRaw(t *testing.T) {
	tmpDir := t.TempDir()
	instance := NewInstance(tmpDir)

	// Create file with known content
	sharedDir := filepath.Join(tmpDir, "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "key1: value1\nkey2: value2\n"
	if err := os.WriteFile(filepath.Join(sharedDir, "test.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	raw, err := instance.ReadSharedRaw("test")
	if err != nil {
		t.Fatalf("ReadSharedRaw() error = %v", err)
	}
	if raw != content {
		t.Errorf("ReadSharedRaw() = %q, want %q", raw, content)
	}
}

func TestWriteShared_InvalidNamespace(t *testing.T) {
	tmpDir := t.TempDir()
	instance := NewInstance(tmpDir)

	err := instance.WriteShared("-invalid", "key", "value")
	if err == nil {
		t.Error("WriteShared() expected error for invalid namespace")
	}
}
