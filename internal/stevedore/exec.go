//go:build unix

package stevedore

import (
	"context"
	"os/exec"
	"sync"
	"syscall"
)

// execMu protects against the zombie reaper racing with exec.Cmd.Wait().
//
// Problem: when running as PID 1, the zombie reaper calls Wait4(-1, WNOHANG)
// to reap orphaned children. If this runs concurrently with exec.Cmd.Wait()
// (which calls waitid on a specific PID), the reaper can steal the child before
// Go's exec package collects it, causing "waitid: no child processes" errors.
//
// Solution: exec.Cmd.Run() holds a read lock, and the zombie reaper holds
// a write lock. This allows concurrent command execution (multiple readers)
// while ensuring the reaper only runs when no commands are in-flight.
var execMu sync.RWMutex

// newCommand creates an exec.Cmd with process group isolation.
//
// On context cancellation, the entire process group is killed (SIGKILL),
// not just the direct child process. This prevents orphaned grandchildren
// (e.g., ssh spawned by git fetch) from surviving after the parent is killed
// and accumulating as zombie processes.
func newCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Kill the entire process group, not just the leader
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return cmd
}

// runCommand runs an exec.Cmd while holding the exec read lock to prevent
// the zombie reaper from stealing the child process.
func runCommand(cmd *exec.Cmd) error {
	execMu.RLock()
	defer execMu.RUnlock()
	return cmd.Run()
}
