//go:build !unix

package stevedore

import (
	"context"
	"os/exec"
)

// newCommand creates an exec.Cmd. On non-Unix platforms, process group
// isolation is not available, so this is a plain exec.CommandContext.
func newCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
