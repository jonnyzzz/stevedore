//go:build unix

package stevedore

import (
	"context"
	"os/exec"
	"syscall"
)

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
