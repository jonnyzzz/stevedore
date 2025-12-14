package stevedore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindComposeEntrypoint_PrefersDockerComposeYAML(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "docker-compose.yml"), []byte("services: {}"), 0o644); err != nil {
		t.Fatalf("write docker-compose.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docker-compose.yaml"), []byte("services: {}"), 0o644); err != nil {
		t.Fatalf("write docker-compose.yaml: %v", err)
	}

	path, err := FindComposeEntrypoint(root)
	if err != nil {
		t.Fatalf("FindComposeEntrypoint: %v", err)
	}
	if filepath.Base(path) != "docker-compose.yaml" {
		t.Fatalf("expected docker-compose.yaml, got %s", filepath.Base(path))
	}
}

func TestFindComposeEntrypoint_FallsBackToDockerComposeYML(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "docker-compose.yml"), []byte("services: {}"), 0o644); err != nil {
		t.Fatalf("write docker-compose.yml: %v", err)
	}

	path, err := FindComposeEntrypoint(root)
	if err != nil {
		t.Fatalf("FindComposeEntrypoint: %v", err)
	}
	if filepath.Base(path) != "docker-compose.yml" {
		t.Fatalf("expected docker-compose.yml, got %s", filepath.Base(path))
	}
}

func TestFindComposeEntrypoint_SupportsLegacyStevedoreYAML(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "stevedore.yaml"), []byte("services: {}"), 0o644); err != nil {
		t.Fatalf("write stevedore.yaml: %v", err)
	}

	path, err := FindComposeEntrypoint(root)
	if err != nil {
		t.Fatalf("FindComposeEntrypoint: %v", err)
	}
	if filepath.Base(path) != "stevedore.yaml" {
		t.Fatalf("expected stevedore.yaml, got %s", filepath.Base(path))
	}
}

func TestFindComposeEntrypoint_ErrorsWhenMissing(t *testing.T) {
	root := t.TempDir()

	if _, err := FindComposeEntrypoint(root); err == nil {
		t.Fatalf("expected error")
	}
}
