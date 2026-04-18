//go:build linux

package stevedore

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// setSubreaper makes the current process a subreaper for orphaned descendants.
// This allows the test process to adopt orphan processes (same as PID 1 would),
// without actually being PID 1.
func setSubreaper(t *testing.T) {
	t.Helper()
	const prSetChildSubreaper = 36
	_, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0)
	if errno != 0 {
		t.Skipf("prctl(PR_SET_CHILD_SUBREAPER) failed: %v (may need Linux 3.4+)", errno)
	}
}

// countZombieChildren counts zombie processes whose parent is this process.
func countZombieChildren(t *testing.T) int {
	t.Helper()
	myPid := os.Getpid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatalf("cannot read /proc: %v", err)
	}
	count := 0
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		// /proc/pid/stat format: pid (comm) state ppid ...
		s := string(data)
		closeParen := strings.LastIndex(s, ")")
		if closeParen < 0 || closeParen+4 >= len(s) {
			continue
		}
		fields := strings.Fields(s[closeParen+2:])
		if len(fields) < 2 {
			continue
		}
		state := fields[0]
		ppid, _ := strconv.Atoi(fields[1])
		if state == "Z" && ppid == myPid {
			count++
		}
	}
	return count
}

// spawnOrphan creates a child process that backgrounds a grandchild, then exits.
// The grandchild gets reparented to the subreaper (our test process) and becomes
// a zombie when it exits.
//
// Uses runCommand so the exec RWMutex read lock is held: otherwise the
// concurrent reaper goroutine (which holds the write lock during reapZombies)
// can win the race to wait4 the sh child, and Go's exec.Cmd.Wait returns
// "waitid: no child processes". That was the CI flake on earlier runs.
func spawnOrphan(t *testing.T) {
	t.Helper()
	cmd := newCommand(context.Background(), "sh", "-c", "sleep 0.2 &")
	if err := runCommand(cmd); err != nil {
		t.Fatalf("failed to spawn orphan: %v", err)
	}
}

// TestZombieAccumulation verifies that orphaned processes accumulate as zombies
// when no reaper is running. This reproduces the production issue where stevedore
// accumulated 17,000+ zombie git/ssh processes.
func TestZombieAccumulation(t *testing.T) {
	setSubreaper(t)
	initial := countZombieChildren(t)

	// Create orphan processes
	const orphanCount = 3
	for i := 0; i < orphanCount; i++ {
		spawnOrphan(t)
	}

	// Wait for orphans to exit and become zombies
	time.Sleep(500 * time.Millisecond)

	zombies := countZombieChildren(t) - initial
	if zombies < orphanCount {
		t.Fatalf("expected at least %d zombies, got %d — orphan accumulation not reproduced", orphanCount, zombies)
	}
	t.Logf("Confirmed: %d zombie(s) accumulated without reaper (initial: %d)", zombies, initial)

	// Clean up: manually reap so we don't pollute other tests
	reapZombies()
}

// TestZombieReaper verifies that the zombie reaper cleans up orphaned zombie processes.
func TestZombieReaper(t *testing.T) {
	setSubreaper(t)

	// Start the reaper before creating orphans
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go zombieReaperLoop(ctx)

	// Give reaper time to set up signal handler
	time.Sleep(50 * time.Millisecond)

	initial := countZombieChildren(t)

	// Create orphans that will become zombies
	const orphanCount = 5
	for i := 0; i < orphanCount; i++ {
		spawnOrphan(t)
	}

	// Wait for orphans to exit and reaper to clean up.
	// The reaper gets SIGCHLD when each orphan exits and reaps immediately.
	time.Sleep(1 * time.Second)

	zombies := countZombieChildren(t) - initial
	if zombies > 0 {
		t.Errorf("expected 0 zombies with reaper running, got %d", zombies)
	} else {
		t.Logf("Confirmed: reaper cleaned up all zombies (initial: %d)", initial)
	}
}

// TestZombieReaper_DoesNotStealExecChildren verifies that the zombie reaper
// does not interfere with exec.Cmd.Run() by stealing its child process.
// This was the root cause of "waitid: no child processes" errors in production.
func TestZombieReaper_DoesNotStealExecChildren(t *testing.T) {
	setSubreaper(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go zombieReaperLoop(ctx)
	time.Sleep(50 * time.Millisecond)

	// Run many concurrent exec.Cmd operations while the reaper is active.
	// Without the RWMutex fix, some of these would fail with "waitid: no child processes".
	const concurrency = 10
	const iterations = 20
	errors := make(chan error, concurrency*iterations)

	for g := 0; g < concurrency; g++ {
		go func() {
			for i := 0; i < iterations; i++ {
				cmd := newCommand(ctx, "true")
				if err := runCommand(cmd); err != nil {
					errors <- fmt.Errorf("runCommand(true) failed: %w", err)
					return
				}
				// Spawn an orphan between commands to trigger SIGCHLD / reaper activity
				spawnOrphan(t)
			}
		}()
	}

	// Wait for all goroutines (they either finish or push an error)
	time.Sleep(3 * time.Second)
	close(errors)

	var fails []error
	for err := range errors {
		fails = append(fails, err)
	}
	if len(fails) > 0 {
		t.Errorf("%d/%d commands failed (expected 0 with RWMutex fix):", len(fails), concurrency*iterations)
		for _, err := range fails[:min(5, len(fails))] {
			t.Errorf("  %v", err)
		}
	} else {
		t.Logf("All %d concurrent commands succeeded with reaper running", concurrency*iterations)
	}
}

// TestReapZombies_DirectCall verifies that reapZombies() correctly reaps zombie processes.
func TestReapZombies_DirectCall(t *testing.T) {
	setSubreaper(t)
	initial := countZombieChildren(t)

	const orphanCount = 3
	for i := 0; i < orphanCount; i++ {
		spawnOrphan(t)
	}

	time.Sleep(500 * time.Millisecond)

	before := countZombieChildren(t) - initial
	if before < orphanCount {
		t.Fatalf("expected at least %d zombies before reap, got %d", orphanCount, before)
	}

	reaped := reapZombies()
	if reaped < orphanCount {
		t.Errorf("expected to reap at least %d, reaped %d", orphanCount, reaped)
	}

	after := countZombieChildren(t) - initial
	if after > 0 {
		t.Errorf("expected 0 zombies after reap, got %d", after)
	}
	t.Logf("Reaped %d zombie(s), %d remaining", reaped, after)
}
