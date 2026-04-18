package stevedore

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
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

// TestSelfUpdate_executeSystemdManaged_callsDockerKill verifies the fix for
// docker-exec invocations: the `stevedore self-update` CLI runs in an exec'd
// subprocess, where plain os.Exit would only kill the subprocess and leave
// the daemon running. Issuing `docker kill` against the mounted socket kills
// the whole container; systemd's Restart=always then brings it back with the
// new image.
func TestSelfUpdate_executeSystemdManaged_callsDockerKill(t *testing.T) {
	origDelay := systemdExitDelay
	origKill := killSelfContainerFn
	origExit := exitProcessFn
	t.Cleanup(func() {
		systemdExitDelay = origDelay
		killSelfContainerFn = origKill
		exitProcessFn = origExit
	})

	systemdExitDelay = 10 * time.Millisecond

	var killedWith atomic.Value
	killSelfContainerFn = func(containerName string) error {
		killedWith.Store(containerName)
		return nil
	}

	// Record exit codes instead of actually exiting. Block on the channel so
	// the goroutine parks without dying.
	exitCh := make(chan int, 2)
	exitProcessFn = func(code int) {
		exitCh <- code
		select {} // never return — the real os.Exit never returns either
	}

	s := NewSelfUpdate(NewInstance(t.TempDir()), SelfUpdateConfig{ContainerName: "test-stevedore"})
	if err := s.executeSystemdManaged("stevedore:latest"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// We expect: delay 10ms → kill ok → 500ms grace → exit(0).
	select {
	case code := <-exitCh:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("exit fn was never called")
	}

	if got, _ := killedWith.Load().(string); got != "test-stevedore" {
		t.Fatalf("kill called with %q, want %q", got, "test-stevedore")
	}
}

// TestSelfUpdate_executeSystemdManaged_fallsBackToExitWhenKillFails verifies
// that if `docker kill` fails (e.g. docker.sock unavailable), we still exit —
// even though the container won't restart, the operator at least sees an
// error and can investigate.
func TestSelfUpdate_executeSystemdManaged_fallsBackToExitWhenKillFails(t *testing.T) {
	origDelay := systemdExitDelay
	origKill := killSelfContainerFn
	origExit := exitProcessFn
	t.Cleanup(func() {
		systemdExitDelay = origDelay
		killSelfContainerFn = origKill
		exitProcessFn = origExit
	})

	systemdExitDelay = 10 * time.Millisecond

	killSelfContainerFn = func(containerName string) error {
		return errForTest("docker not available")
	}

	exitCh := make(chan int, 2)
	exitProcessFn = func(code int) {
		exitCh <- code
		select {}
	}

	s := NewSelfUpdate(NewInstance(t.TempDir()), SelfUpdateConfig{ContainerName: "whatever"})
	if err := s.executeSystemdManaged("stevedore:latest"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case code := <-exitCh:
		if code != 1 {
			t.Fatalf("exit code = %d, want 1 for kill failure", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("exit fn was never called after kill failure")
	}
}

// errForTest is a tiny helper for tests that need a stable error value.
type errForTest string

func (e errForTest) Error() string { return string(e) }
