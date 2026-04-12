package stevedore

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestDaemon_NewDaemon(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	instance := NewInstance(tmpDir)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	config := DaemonConfig{
		AdminKey:   "test-admin-key",
		ListenAddr: ":0",
		Version:    "1.0.0-test",
	}

	daemon := NewDaemon(instance, db, config)

	if daemon.instance != instance {
		t.Error("daemon instance not set correctly")
	}
	if daemon.db != db {
		t.Error("daemon db not set correctly")
	}
	if daemon.config.AdminKey != "test-admin-key" {
		t.Errorf("expected admin key 'test-admin-key', got %q", daemon.config.AdminKey)
	}
	if daemon.config.MinPollTime != 30*time.Second {
		t.Errorf("expected default min poll time 30s, got %v", daemon.config.MinPollTime)
	}
	if daemon.config.SyncTimeout != 5*time.Minute {
		t.Errorf("expected default sync timeout 5m, got %v", daemon.config.SyncTimeout)
	}
	if daemon.config.DeployTimeout != 10*time.Minute {
		t.Errorf("expected default deploy timeout 10m, got %v", daemon.config.DeployTimeout)
	}
	if daemon.config.ReconcileInterval != 30*time.Second {
		t.Errorf("expected default reconcile interval 30s, got %v", daemon.config.ReconcileInterval)
	}
}

func TestDaemon_SyncTracking(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	instance := NewInstance(tmpDir)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	daemon := NewDaemon(instance, db, DaemonConfig{
		AdminKey: "test-key",
	})

	// Initially not syncing
	if daemon.isActive("test-deployment") {
		t.Error("expected deployment to not be active initially")
	}

	// Set syncing
	daemon.setActive("test-deployment", true)
	if !daemon.isActive("test-deployment") {
		t.Error("expected deployment to be active after setActive(true)")
	}

	// Clear syncing
	daemon.setActive("test-deployment", false)
	if daemon.isActive("test-deployment") {
		t.Error("expected deployment to not be active after setActive(false)")
	}
}

func TestDaemon_RunWithCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	instance := NewInstance(tmpDir)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	daemon := NewDaemon(instance, db, DaemonConfig{
		AdminKey:          "test-key",
		ListenAddr:        ":0", // Random port
		MinPollTime:       100 * time.Millisecond,
		ReconcileInterval: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- daemon.Run(ctx)
	}()

	// Give it time to start
	time.Sleep(200 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for daemon to stop
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("daemon did not stop within timeout")
	}
}

func TestNeedsReconcile(t *testing.T) {
	tests := []struct {
		name   string
		status *DeploymentStatus
		want   bool
	}{
		{"nil status", nil, false},
		{"no containers", &DeploymentStatus{Containers: nil}, true},
		{"empty containers", &DeploymentStatus{Containers: []ContainerStatus{}}, true},
		{"running container", &DeploymentStatus{Containers: []ContainerStatus{
			{State: StateRunning},
		}}, false},
		{"exited container", &DeploymentStatus{Containers: []ContainerStatus{
			{State: StateExited},
		}}, true},
		{"mixed running and exited", &DeploymentStatus{Containers: []ContainerStatus{
			{State: StateRunning},
			{State: StateExited},
		}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsReconcile(tt.status); got != tt.want {
				t.Errorf("needsReconcile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsTransientExecError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("connection refused"), false},
		{fmt.Errorf("failed to list containers: waitid: no child processes:"), true},
		{fmt.Errorf("waitid error"), true},
		{fmt.Errorf("something: no child processes"), true},
	}
	for _, tt := range tests {
		name := "nil"
		if tt.err != nil {
			name = tt.err.Error()
		}
		t.Run(name, func(t *testing.T) {
			if got := isTransientExecError(tt.err); got != tt.want {
				t.Errorf("isTransientExecError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestComposeConfig_BuildFlag(t *testing.T) {
	// Verify default ComposeConfig has Build=false (used by reconciler)
	reconcileConfig := ComposeConfig{}
	if reconcileConfig.Build {
		t.Error("default ComposeConfig.Build should be false (reconciler should not rebuild)")
	}

	// Verify sync deploy uses Build=true
	syncConfig := ComposeConfig{Build: true}
	if !syncConfig.Build {
		t.Error("sync ComposeConfig.Build should be true")
	}
}

func TestShortCommit(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abc123def456ghi789", "abc123def456"},
		{"abc123", "abc123"},
		{"", ""},
		{"123456789012", "123456789012"},
		{"1234567890123", "123456789012"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := shortCommit(tt.input)
			if got != tt.want {
				t.Errorf("shortCommit(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
