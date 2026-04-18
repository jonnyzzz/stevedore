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

func TestServicesMissingInit_PassesWhenAllHaveInit(t *testing.T) {
	trueVal := true
	services := map[string]composeConfigService{
		"web":    {Init: &trueVal},
		"worker": {Init: &trueVal},
	}
	if got := servicesMissingInit(services); len(got) != 0 {
		t.Fatalf("expected no missing services, got %v", got)
	}
}

func TestServicesMissingInit_FlagsServicesWithoutInit(t *testing.T) {
	trueVal := true
	falseVal := false
	services := map[string]composeConfigService{
		"web":        {Init: &trueVal},
		"bot":        {},            // missing → must be flagged
		"legacy":     {Init: &falseVal}, // explicit false → must be flagged
		"background": {},            // missing → must be flagged
	}
	got := servicesMissingInit(services)
	want := []string{"background", "bot", "legacy"}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestServicesMissingInit_RespectsOptOutLabel(t *testing.T) {
	services := map[string]composeConfigService{
		"sidecar": {
			Labels: map[string]string{InitEnforceLabel: "false"},
		},
		"sidecar-loud": {
			Labels: map[string]string{InitEnforceLabel: "FALSE"}, // case-insensitive
		},
	}
	if got := servicesMissingInit(services); len(got) != 0 {
		t.Fatalf("opt-out label should skip check, got %v", got)
	}
}

func TestServicesMissingInit_IgnoresUnrelatedLabels(t *testing.T) {
	services := map[string]composeConfigService{
		"web": {
			Labels: map[string]string{"stevedore.ingress.enabled": "true"},
			// no init, no opt-out → still flagged
		},
	}
	got := servicesMissingInit(services)
	if len(got) != 1 || got[0] != "web" {
		t.Fatalf("expected [web], got %v", got)
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
