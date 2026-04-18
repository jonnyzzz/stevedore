package stevedore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WatchdogAction is the recommended action after a PID-pressure check.
type WatchdogAction int

const (
	WatchdogOK WatchdogAction = iota
	WatchdogWarn
	WatchdogRestart
)

// WatchdogConfig holds thresholds and timing for the PID-pressure watchdog.
type WatchdogConfig struct {
	// Interval between watchdog sweeps.
	Interval time.Duration
	// WarnPct is the pids.current/pids.max ratio (0..1) that triggers a warning log.
	WarnPct float64
	// RestartPct is the ratio that triggers an automatic restart.
	RestartPct float64
	// MinRestartGap is the minimum time between auto-restarts of the same deployment.
	MinRestartGap time.Duration
	// CgroupRoot is the path of the unified cgroup v2 hierarchy. Defaults to /sys/fs/cgroup.
	// Overridable for tests.
	CgroupRoot string
	// SummarizeEveryN makes the watchdog emit a one-line PID-usage summary every
	// Nth sweep. Zero disables summaries; negative falls back to the default (10
	// sweeps ≈ 5 min at a 30-second Interval). A summary gives operators trend
	// visibility without the noise of logging every sweep.
	SummarizeEveryN int
}

// Watchdog monitors PID pressure inside managed container cgroups and restarts
// deployments before pids.max is reached.
//
// Motivation: a container that leaks zombies (orphaned subprocesses) will fill
// its cgroup's PID slot table until fork() starts returning EAGAIN. Once that
// happens the service is effectively dead — it cannot exec anything, including
// its own health check. We want to catch the trend, not the cliff.
type Watchdog struct {
	instance *Instance
	daemon   *Daemon
	config   WatchdogConfig

	mu            sync.Mutex
	lastRestart   map[string]time.Time
	loggedMissing map[string]bool
	sweepCount    int
}

// NewWatchdog returns a watchdog with defaults filled in.
func NewWatchdog(instance *Instance, daemon *Daemon, config WatchdogConfig) *Watchdog {
	if config.Interval <= 0 {
		config.Interval = 30 * time.Second
	}
	if config.WarnPct <= 0 {
		config.WarnPct = 0.5
	}
	if config.RestartPct <= 0 {
		config.RestartPct = 0.8
	}
	if config.MinRestartGap <= 0 {
		config.MinRestartGap = 10 * time.Minute
	}
	if config.CgroupRoot == "" {
		config.CgroupRoot = "/sys/fs/cgroup"
	}
	if config.SummarizeEveryN == 0 {
		// Default ≈ 5 min of visibility at the 30 s interval.
		config.SummarizeEveryN = 10
	}
	return &Watchdog{
		instance:      instance,
		daemon:        daemon,
		config:        config,
		lastRestart:   make(map[string]time.Time),
		loggedMissing: make(map[string]bool),
	}
}

// Run blocks until ctx is canceled, sweeping all managed deployments on each tick.
func (w *Watchdog) Run(ctx context.Context) {
	log.Printf("PID watchdog started (interval=%s, warn=%.0f%%, restart=%.0f%%)",
		w.config.Interval,
		w.config.WarnPct*100,
		w.config.RestartPct*100,
	)

	ticker := time.NewTicker(w.config.Interval)
	defer ticker.Stop()

	w.sweep(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Printf("PID watchdog stopping")
			return
		case <-ticker.C:
			w.sweep(ctx)
		}
	}
}

// deploymentReading is the worst PID reading observed for a deployment in one sweep.
type deploymentReading struct {
	Deployment  string
	ContainerID string // short id of the worst container
	Current     int64
	Max         int64
}

// Ratio returns the pids.current / pids.max fraction, or 0 when Max is unset.
func (r deploymentReading) Ratio() float64 {
	if r.Max <= 0 {
		return 0
	}
	return float64(r.Current) / float64(r.Max)
}

// sweep checks every managed deployment once, then maybe emits a summary.
func (w *Watchdog) sweep(ctx context.Context) {
	if w.daemon == nil {
		return
	}

	deployments, err := w.instance.ListDeployments()
	if err != nil {
		log.Printf("Watchdog: failed to list deployments: %v", err)
		return
	}

	var readings []deploymentReading
	for _, deployment := range deployments {
		if deployment == "stevedore" {
			// Don't auto-restart ourselves. Stevedore runs its own zombie reaper.
			continue
		}
		if w.daemon.isActive(deployment) {
			continue
		}
		if r, ok := w.checkDeployment(ctx, deployment); ok {
			readings = append(readings, r)
		}
	}

	w.sweepCount++
	if w.config.SummarizeEveryN > 0 && w.sweepCount%w.config.SummarizeEveryN == 0 {
		w.logSummary(readings)
	}
}

// checkDeployment inspects one deployment and acts on the worst-pressure container.
// Returns (reading, true) when any container yielded a readable ratio, (zero, false)
// otherwise — summaries should only include deployments we could actually measure.
func (w *Watchdog) checkDeployment(ctx context.Context, deployment string) (deploymentReading, bool) {
	projectName := ComposeProjectName(deployment)
	containerIDs, err := w.instance.listProjectContainerIDs(ctx, projectName)
	if err != nil {
		return deploymentReading{}, false
	}

	worst := deploymentReading{Deployment: deployment}
	worstFound := false

	for _, cid := range containerIDs {
		reading, err := w.readCgroupPids(cid)
		if err != nil {
			if !w.noteMissing(cid) {
				log.Printf("Watchdog: cannot read cgroup pids for %s (%s): %v", deployment, shortCID(cid), err)
			}
			continue
		}
		if reading.Max <= 0 {
			continue // no limit → nothing to pressure against
		}
		ratio := float64(reading.Current) / float64(reading.Max)
		if !worstFound || ratio > worst.Ratio() {
			worstFound = true
			worst = deploymentReading{
				Deployment:  deployment,
				ContainerID: shortCID(cid),
				Current:     reading.Current,
				Max:         reading.Max,
			}
		}
	}

	if !worstFound {
		return deploymentReading{}, false
	}

	switch w.classify(worst.Ratio()) {
	case WatchdogWarn:
		log.Printf("Watchdog: %s container %s pid pressure %d/%d (%.0f%%) — warning",
			worst.Deployment, worst.ContainerID, worst.Current, worst.Max, worst.Ratio()*100)
	case WatchdogRestart:
		log.Printf("Watchdog: %s container %s pid pressure %d/%d (%.0f%%) — triggering restart",
			worst.Deployment, worst.ContainerID, worst.Current, worst.Max, worst.Ratio()*100)
		w.restart(ctx, worst.Deployment)
	}
	return worst, true
}

// logSummary emits a one-line per-deployment PID usage report, sorted by ratio
// descending so the most-stressed container is always first.
func (w *Watchdog) logSummary(readings []deploymentReading) {
	if len(readings) == 0 {
		return
	}
	sorted := make([]deploymentReading, len(readings))
	copy(sorted, readings)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Ratio() > sorted[j].Ratio()
	})

	var b strings.Builder
	for i, r := range sorted {
		if i > 0 {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "%s=%d/%d(%.0f%%)", r.Deployment, r.Current, r.Max, r.Ratio()*100)
	}
	log.Printf("Watchdog: pid usage — %s", b.String())
}

func (w *Watchdog) classify(ratio float64) WatchdogAction {
	switch {
	case ratio >= w.config.RestartPct:
		return WatchdogRestart
	case ratio >= w.config.WarnPct:
		return WatchdogWarn
	default:
		return WatchdogOK
	}
}

// restart stops and redeploys a deployment, respecting the per-deployment cooldown.
func (w *Watchdog) restart(ctx context.Context, deployment string) {
	w.mu.Lock()
	last := w.lastRestart[deployment]
	if !last.IsZero() && time.Since(last) < w.config.MinRestartGap {
		w.mu.Unlock()
		log.Printf("Watchdog: skipping restart of %s — cooldown (last %s ago)", deployment, time.Since(last).Truncate(time.Second))
		return
	}
	w.lastRestart[deployment] = time.Now()
	w.mu.Unlock()

	w.daemon.setActive(deployment, true)
	defer w.daemon.setActive(deployment, false)

	stopCtx, stopCancel := context.WithTimeout(ctx, w.daemon.config.DeployTimeout)
	defer stopCancel()
	if err := w.instance.Stop(stopCtx, deployment, ComposeConfig{}); err != nil {
		log.Printf("Watchdog: stop failed for %s: %v", deployment, err)
		return
	}

	deployCtx, deployCancel := context.WithTimeout(ctx, w.daemon.config.DeployTimeout)
	defer deployCancel()
	result, err := w.instance.Deploy(deployCtx, deployment, ComposeConfig{})
	if err != nil {
		log.Printf("Watchdog: redeploy failed for %s: %v", deployment, err)
		return
	}
	log.Printf("Watchdog: restarted %s (services=%v)", deployment, result.Services)
}

// noteMissing returns true if we've already logged about this cid. Keeps logs quiet.
func (w *Watchdog) noteMissing(cid string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.loggedMissing[cid] {
		return true
	}
	w.loggedMissing[cid] = true
	return false
}

// cgroupPidsReading is one sample of pids.current and pids.max for a cgroup.
type cgroupPidsReading struct {
	Current int64
	Max     int64 // 0 means unlimited ("max")
}

// readCgroupPids finds the cgroup directory for a container and reads its pids counters.
// Supports the common cgroup v2 layouts; returns an error if no layout matches.
func (w *Watchdog) readCgroupPids(containerID string) (cgroupPidsReading, error) {
	dir := w.findCgroupDir(containerID)
	if dir == "" {
		return cgroupPidsReading{}, errors.New("cgroup path not found")
	}

	current, err := readInt(filepath.Join(dir, "pids.current"))
	if err != nil {
		return cgroupPidsReading{}, fmt.Errorf("read pids.current: %w", err)
	}
	max, err := readPidsMax(filepath.Join(dir, "pids.max"))
	if err != nil {
		return cgroupPidsReading{}, fmt.Errorf("read pids.max: %w", err)
	}
	return cgroupPidsReading{Current: current, Max: max}, nil
}

// findCgroupDir returns the first directory under CgroupRoot that matches a known
// docker layout for this container, or "" if none exist.
func (w *Watchdog) findCgroupDir(containerID string) string {
	candidates := []string{
		// cgroup v2 + systemd driver (Debian/Ubuntu default)
		filepath.Join(w.config.CgroupRoot, "system.slice", "docker-"+containerID+".scope"),
		// cgroup v2 + cgroupfs driver
		filepath.Join(w.config.CgroupRoot, "docker", containerID),
		// cgroup v1 pids subsystem
		filepath.Join(w.config.CgroupRoot, "pids", "docker", containerID),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return ""
}

func readInt(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
}

// readPidsMax reads pids.max, which is either an integer or the literal "max" (unlimited).
// Returns 0 for "max" so callers can detect "no limit".
func readPidsMax(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(data))
	if s == "max" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

func shortCID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// listProjectContainerIDs returns the full IDs of every container belonging to a compose project.
func (i *Instance) listProjectContainerIDs(ctx context.Context, projectName string) ([]string, error) {
	args := []string{
		"ps", "-aq", "--no-trunc",
		"--filter", "label=com.docker.compose.project=" + projectName,
	}
	cmd := newCommand(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := runCommand(cmd); err != nil {
		return nil, fmt.Errorf("docker ps failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ids = append(ids, line)
	}
	return ids, nil
}
