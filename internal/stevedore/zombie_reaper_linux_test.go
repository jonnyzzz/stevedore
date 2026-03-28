//go:build linux

package stevedore

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
func spawnOrphan(t *testing.T) {
	t.Helper()
	cmd := exec.Command("sh", "-c", "sleep 0.2 &")
	if err := cmd.Run(); err != nil {
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
