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

// countChildProcesses counts non-zombie child processes of this process.
func countChildProcesses(t *testing.T) int {
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
		s := string(data)
		closeParen := strings.LastIndex(s, ")")
		if closeParen < 0 || closeParen+4 >= len(s) {
			continue
		}
		fields := strings.Fields(s[closeParen+2:])
		if len(fields) < 2 {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		if ppid == myPid && fields[0] != "Z" {
			count++
		}
	}
	return count
}

// TestContextCancel_WithoutProcessGroup shows that cancelling a context kills
// only the direct child, leaving grandchildren (like ssh spawned by git) alive
// as orphans. This is the root cause of the zombie accumulation.
func TestContextCancel_WithoutProcessGroup(t *testing.T) {
	setSubreaper(t)

	initial := countChildProcesses(t)

	// Spawn a parent that creates a long-running child, then cancel the context.
	// "sh -c 'sleep 30 &; sleep 30'" — sh runs sleep 30 in background, then
	// runs another sleep 30 in foreground. Killing sh leaves the background sleep alive.
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Let the child start its grandchild
	time.Sleep(100 * time.Millisecond)

	// Cancel kills only "sh", not the background "sleep 30"
	cancel()
	_ = cmd.Wait()

	time.Sleep(100 * time.Millisecond)

	orphans := countChildProcesses(t) - initial
	if orphans < 1 {
		t.Fatalf("expected at least 1 orphaned grandchild, got %d", orphans)
	}
	t.Logf("Without process group: %d orphaned grandchild(ren) after context cancel", orphans)

	// Clean up the orphan
	reapOrphanChildren(t)
}

// TestContextCancel_WithProcessGroup shows that using Setpgid + process group kill
// cleans up the entire process tree, preventing orphans.
func TestContextCancel_WithProcessGroup(t *testing.T) {
	setSubreaper(t)

	initial := countChildProcesses(t)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 30 & sleep 30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Kill the entire process group, not just the parent
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	cancel()
	_ = cmd.Wait()

	time.Sleep(100 * time.Millisecond)

	orphans := countChildProcesses(t) - initial
	if orphans > 0 {
		t.Errorf("expected 0 orphaned grandchildren with process group kill, got %d", orphans)
		reapOrphanChildren(t)
	} else {
		t.Logf("With process group: 0 orphaned grandchildren after context cancel")
	}

	// Reap any zombies from killed processes
	reapZombies()
}

// reapOrphanChildren kills and reaps any child processes of this process.
func reapOrphanChildren(t *testing.T) {
	t.Helper()
	myPid := os.Getpid()
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		s := string(data)
		closeParen := strings.LastIndex(s, ")")
		if closeParen < 0 || closeParen+4 >= len(s) {
			continue
		}
		fields := strings.Fields(s[closeParen+2:])
		if len(fields) < 2 {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		if ppid == myPid {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	time.Sleep(100 * time.Millisecond)
	reapZombies()
}
