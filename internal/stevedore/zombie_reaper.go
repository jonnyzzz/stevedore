//go:build unix

package stevedore

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// StartZombieReaper starts a background goroutine that reaps zombie (defunct) processes.
//
// When running as PID 1 (e.g., in a Docker container without --init), orphaned child
// processes are re-parented to PID 1. If PID 1 doesn't call wait() for these adopted
// children, they accumulate as zombies, eventually exhausting the system's PID space.
//
// This was observed in production: stevedore's periodic git operations (which spawn ssh
// subprocesses) accumulated 17,000+ zombies over 3 weeks, preventing new processes from
// being created.
func StartZombieReaper(ctx context.Context) {
	if os.Getpid() != 1 {
		return
	}
	log.Printf("Starting zombie reaper (running as PID 1)")
	go zombieReaperLoop(ctx)
}

// zombieReaperLoop listens for SIGCHLD and reaps zombie processes.
func zombieReaperLoop(ctx context.Context) {
	ch := make(chan os.Signal, 32)
	signal.Notify(ch, syscall.SIGCHLD)
	defer signal.Stop(ch)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			reapZombies()
		}
	}
}

// reapZombies reaps all currently-waitable zombie child processes.
// Returns the number of processes reaped.
func reapZombies() int {
	reaped := 0
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if pid <= 0 || err != nil {
			break
		}
		reaped++
		log.Printf("Reaped zombie process: pid=%d, exit=%d", pid, status.ExitStatus())
	}
	return reaped
}
