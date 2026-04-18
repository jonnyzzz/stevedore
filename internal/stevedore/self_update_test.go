package stevedore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelfUpdate_IsManagedBySystemd_returnsFalseWhenSentinelAbsent(t *testing.T) {
	root := t.TempDir()
	instance := NewInstance(root)
	if err := os.MkdirAll(instance.SystemDir(), 0o755); err != nil {
		t.Fatalf("mkdir system: %v", err)
	}

	s := NewSelfUpdate(instance, SelfUpdateConfig{})
	if s.IsManagedBySystemd() {
		t.Fatalf("expected false without sentinel")
	}
}

func TestSelfUpdate_IsManagedBySystemd_returnsTrueWhenSentinelPresent(t *testing.T) {
	root := t.TempDir()
	instance := NewInstance(root)
	if err := os.MkdirAll(instance.SystemDir(), 0o755); err != nil {
		t.Fatalf("mkdir system: %v", err)
	}
	sentinel := filepath.Join(instance.SystemDir(), ManagedBySystemdSentinel)
	if err := os.WriteFile(sentinel, nil, 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	s := NewSelfUpdate(instance, SelfUpdateConfig{})
	if !s.IsManagedBySystemd() {
		t.Fatalf("expected true when sentinel %s exists", sentinel)
	}
}

func TestSelfUpdate_executeSystemdManaged_doesNotExitSynchronously(t *testing.T) {
	// Override the exit delay so the goroutine never fires during the test.
	orig := systemdExitDelay
	t.Cleanup(func() { systemdExitDelay = orig })
	systemdExitDelay = 10 * 60 * 1_000_000_000 // 10 minutes — effectively never

	s := NewSelfUpdate(NewInstance(t.TempDir()), SelfUpdateConfig{})
	// If executeSystemdManaged called os.Exit directly, the test process would
	// die. The fact that we return here means the exit is deferred as intended.
	if err := s.executeSystemdManaged("stevedore:latest"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
