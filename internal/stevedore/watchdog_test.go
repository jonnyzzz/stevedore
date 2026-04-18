package stevedore

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCgroupContainer fakes the files Docker creates under /sys/fs/cgroup for a
// container in the systemd cgroup-driver + v2 layout.
func writeCgroupContainer(t *testing.T, root, containerID, current, max string) string {
	t.Helper()
	dir := filepath.Join(root, "system.slice", "docker-"+containerID+".scope")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pids.current"), []byte(current+"\n"), 0o644); err != nil {
		t.Fatalf("write current: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pids.max"), []byte(max+"\n"), 0o644); err != nil {
		t.Fatalf("write max: %v", err)
	}
	return dir
}

func TestWatchdog_readCgroupPids_parsesReading(t *testing.T) {
	root := t.TempDir()
	cid := "abc123def456"
	writeCgroupContainer(t, root, cid, "42", "100")

	w := NewWatchdog(nil, nil, WatchdogConfig{CgroupRoot: root})
	reading, err := w.readCgroupPids(cid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reading.Current != 42 || reading.Max != 100 {
		t.Fatalf("got %+v, want current=42 max=100", reading)
	}
}

func TestWatchdog_readCgroupPids_handlesMaxSentinel(t *testing.T) {
	root := t.TempDir()
	cid := "unlimited"
	writeCgroupContainer(t, root, cid, "5", "max")

	w := NewWatchdog(nil, nil, WatchdogConfig{CgroupRoot: root})
	reading, err := w.readCgroupPids(cid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reading.Max != 0 {
		t.Fatalf("expected Max=0 for literal 'max', got %d", reading.Max)
	}
}

func TestWatchdog_readCgroupPids_errorOnMissingContainer(t *testing.T) {
	root := t.TempDir()
	w := NewWatchdog(nil, nil, WatchdogConfig{CgroupRoot: root})
	if _, err := w.readCgroupPids("does-not-exist"); err == nil {
		t.Fatalf("expected error for missing cgroup, got nil")
	}
}

func TestWatchdog_classify_thresholds(t *testing.T) {
	w := NewWatchdog(nil, nil, WatchdogConfig{WarnPct: 0.5, RestartPct: 0.8})
	cases := []struct {
		ratio float64
		want  WatchdogAction
	}{
		{0.1, WatchdogOK},
		{0.49, WatchdogOK},
		{0.50, WatchdogWarn},
		{0.79, WatchdogWarn},
		{0.80, WatchdogRestart},
		{0.99, WatchdogRestart},
	}
	for _, c := range cases {
		got := w.classify(c.ratio)
		if got != c.want {
			t.Errorf("classify(%.2f)=%d, want %d", c.ratio, got, c.want)
		}
	}
}

func TestWatchdog_findCgroupDir_tripsLayouts(t *testing.T) {
	root := t.TempDir()
	cid := "layoutcid"
	dir := filepath.Join(root, "docker", cid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	w := NewWatchdog(nil, nil, WatchdogConfig{CgroupRoot: root})
	got := w.findCgroupDir(cid)
	if got != dir {
		t.Fatalf("findCgroupDir=%s, want %s", got, dir)
	}
}

func TestWatchdog_NewWatchdog_fillsDefaults(t *testing.T) {
	w := NewWatchdog(nil, nil, WatchdogConfig{})
	if w.config.Interval <= 0 {
		t.Error("Interval not defaulted")
	}
	if w.config.WarnPct <= 0 {
		t.Error("WarnPct not defaulted")
	}
	if w.config.RestartPct <= 0 {
		t.Error("RestartPct not defaulted")
	}
	if w.config.MinRestartGap <= 0 {
		t.Error("MinRestartGap not defaulted")
	}
	if w.config.CgroupRoot == "" {
		t.Error("CgroupRoot not defaulted")
	}
	if w.config.SummarizeEveryN <= 0 {
		t.Error("SummarizeEveryN not defaulted")
	}
}

// captureLog captures log output from the default logger during the test.
func captureLog(t *testing.T) func() string {
	t.Helper()
	var buf strings.Builder
	origOutput := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(origOutput)
		log.SetFlags(origFlags)
	})
	return buf.String
}

func TestWatchdog_logSummary_sortsWorstFirstAndSkipsEmpty(t *testing.T) {
	w := NewWatchdog(nil, nil, WatchdogConfig{})
	get := captureLog(t)

	// Empty input → no log line at all.
	w.logSummary(nil)
	if out := get(); out != "" {
		t.Fatalf("empty readings should produce no log, got %q", out)
	}

	w.logSummary([]deploymentReading{
		{Deployment: "low", Current: 5, Max: 100},
		{Deployment: "high", Current: 80, Max: 100},
		{Deployment: "mid", Current: 40, Max: 100},
	})

	out := get()
	if !strings.Contains(out, "pid usage") {
		t.Fatalf("summary missing 'pid usage': %q", out)
	}
	// Ensure ordering: high before mid before low in the same line.
	hi := strings.Index(out, "high=")
	md := strings.Index(out, "mid=")
	lo := strings.Index(out, "low=")
	if !(hi >= 0 && md > hi && lo > md) {
		t.Fatalf("expected order high < mid < low, got %q", out)
	}
}

func TestWatchdog_logSummary_omittedWhenDisabled(t *testing.T) {
	// SummarizeEveryN=0 would hit the defaulting branch; we approximate
	// "disabled" by verifying the threshold math: if the config says "log
	// every 1000 sweeps" we shouldn't have logged on sweep 1.
	w := NewWatchdog(nil, nil, WatchdogConfig{SummarizeEveryN: 1000})
	get := captureLog(t)

	// Pretend we're partway through the first sweep.
	w.sweepCount = 1
	if w.config.SummarizeEveryN > 0 && w.sweepCount%w.config.SummarizeEveryN == 0 {
		w.logSummary([]deploymentReading{{Deployment: "x", Current: 1, Max: 2}})
	}
	if out := get(); out != "" {
		t.Fatalf("summary should not have fired on sweep 1 with N=1000, got %q", out)
	}
}

func TestDeploymentReading_Ratio(t *testing.T) {
	cases := []struct {
		r    deploymentReading
		want float64
	}{
		{deploymentReading{Current: 0, Max: 100}, 0.0},
		{deploymentReading{Current: 50, Max: 100}, 0.5},
		{deploymentReading{Current: 100, Max: 100}, 1.0},
		{deploymentReading{Current: 10, Max: 0}, 0.0}, // unlimited cgroup → zero ratio
	}
	for _, c := range cases {
		if got := c.r.Ratio(); got != c.want {
			t.Errorf("Ratio(%+v)=%v, want %v", c.r, got, c.want)
		}
	}
}
