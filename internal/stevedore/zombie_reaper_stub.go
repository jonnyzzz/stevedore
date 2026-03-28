//go:build !unix

package stevedore

import "context"

// StartZombieReaper is a no-op on non-Unix platforms.
// Zombie process reaping is only needed on Unix when running as PID 1.
func StartZombieReaper(_ context.Context) {}
