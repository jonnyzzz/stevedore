package stevedore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDBKey_UsesEnvValue(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "env-key")

	key, err := instance.dbKey()
	if err != nil {
		t.Fatalf("dbKey: %v", err)
	}
	if key != "env-key" {
		t.Fatalf("unexpected key: %q", key)
	}
}

func TestDBKey_UsesEnvFile(t *testing.T) {
	instance := NewInstance(t.TempDir())

	keyPath := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(keyPath, []byte("file-key\n"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	t.Setenv("STEVEDORE_DB_KEY_FILE", keyPath)

	key, err := instance.dbKey()
	if err != nil {
		t.Fatalf("dbKey: %v", err)
	}
	if key != "file-key" {
		t.Fatalf("unexpected key: %q", key)
	}
}

func TestDBKey_UsesDefaultPath(t *testing.T) {
	instance := NewInstance(t.TempDir())
	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	if err := os.WriteFile(instance.DBKeyPath(), []byte("default-key\n"), 0o600); err != nil {
		t.Fatalf("write default key file: %v", err)
	}

	key, err := instance.dbKey()
	if err != nil {
		t.Fatalf("dbKey: %v", err)
	}
	if key != "default-key" {
		t.Fatalf("unexpected key: %q", key)
	}
}

func TestDBKey_MissingErrors(t *testing.T) {
	instance := NewInstance(t.TempDir())
	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	if _, err := instance.dbKey(); err == nil {
		t.Fatalf("expected error")
	}
}
