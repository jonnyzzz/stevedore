package integration_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestContainer is the main entry point for integration tests.
// It encapsulates all Docker operations and cleanup logic.
type TestContainer struct {
	t   testing.TB
	ctx context.Context
	r   *Runner

	name   string
	docker *dockerCLI

	// ImageTag is the tag of the built donor container image.
	ImageTag string

	// StateDir is a temporary directory on the host mounted into the container.
	StateDir string

	// RepoRoot is the absolute path to the repository root directory.
	RepoRoot string

	// ContainerPrefix is the unique prefix used for this test run.
	ContainerPrefix string

	// StevedoreContainerName is the name for the stevedore container created by the installer.
	StevedoreContainerName string

	// StevedoreImageTag is the image tag for the stevedore container.
	StevedoreImageTag string
}

// NewTestContainer creates a new test container from the specified Dockerfile.
// It builds the image, starts the container with proper mounts, and registers cleanup.
// If Docker is not available, the test is skipped.
//
// The Dockerfile should be located in tests/integration directory.
// Example: NewTestContainer(t, "Dockerfile.ubuntu")
func NewTestContainer(t testing.TB, dockerfile string) *TestContainer {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	t.Cleanup(cancel)

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not installed")
	}

	r := NewRunner(t)
	docker := &dockerCLI{t: t, ctx: ctx, r: r}

	// Sanitize dockerfile name for use in Docker image/container names
	// Docker requires lowercase names and dots have special meaning
	dockerfileID := sanitizeDockerName(dockerfile)
	prefix := "stevedore-it-" + dockerfileID
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	containerPrefix := prefix + "-" + runID
	containerName := containerPrefix + "-donor"
	imageTag := prefix + ":" + runID

	repoRoot := testRepoRoot(t)
	dockerfilePath := filepath.Join(repoRoot, "tests", "integration", dockerfile)

	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		t.Fatalf("Dockerfile not found: %s", dockerfilePath)
	}

	stateDir := filepath.Join(repoRoot, ".tmp", prefix+"-"+runID)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}

	stevedoreContainerName := containerPrefix + "-stevedore"
	stevedoreImageTag := "stevedore:it-" + containerPrefix

	// Register cleanup in reverse order of creation
	t.Cleanup(func() { _ = os.RemoveAll(stateDir) })
	t.Cleanup(func() { docker.removeImage(stevedoreImageTag) })
	t.Cleanup(func() { docker.stopAndRemoveContainer(stevedoreContainerName) })
	t.Cleanup(func() { docker.removeImage(imageTag) })
	t.Cleanup(func() { docker.stopAndRemoveContainer(containerName) })

	// Clean up any stale containers from previous test runs
	docker.removeContainersByPrefix(prefix + "-")

	// Build the image
	docker.runOK(
		"build",
		"-t", imageTag,
		"-f", dockerfilePath,
		filepath.Dir(dockerfilePath),
	)

	// Start the container
	docker.runOK(
		"run",
		"-d",
		"--name", containerName,
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", repoRoot+":/tmp/stevedore-src:ro",
		"-v", stateDir+":"+stateDir,
		imageTag,
	)

	return &TestContainer{
		t:                      t,
		ctx:                    ctx,
		r:                      r,
		name:                   containerName,
		docker:                 docker,
		ImageTag:               imageTag,
		StateDir:               stateDir,
		RepoRoot:               repoRoot,
		ContainerPrefix:        containerPrefix,
		StevedoreContainerName: stevedoreContainerName,
		StevedoreImageTag:      stevedoreImageTag,
	}
}

// Name returns the container name.
func (c *TestContainer) Name() string {
	return c.name
}

// Exec runs a command inside the container.
func (c *TestContainer) Exec(args ...string) (ExecResult, error) {
	c.t.Helper()

	return c.r.Exec(c.ctx, ExecSpec{
		Cmd:    "docker",
		Args:   append([]string{"exec", c.name}, args...),
		Prefix: "[exec]",
	})
}

// ExecOK runs a command inside the container and fails the test if it fails.
func (c *TestContainer) ExecOK(args ...string) string {
	c.t.Helper()

	res, err := c.Exec(args...)
	if err != nil || res.ExitCode != 0 {
		c.t.Fatalf("docker exec %s %s failed (exit=%d): %v", c.name, strings.Join(args, " "), res.ExitCode, err)
	}
	return res.Output
}

// ExecEnvOK runs a command with environment variables inside the container.
func (c *TestContainer) ExecEnvOK(env map[string]string, args ...string) string {
	c.t.Helper()

	dockerArgs := make([]string, 0, 2+len(env)*2+1+len(args))
	dockerArgs = append(dockerArgs, "exec")

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		dockerArgs = append(dockerArgs, "-e", k+"="+env[k])
	}

	dockerArgs = append(dockerArgs, c.name)
	dockerArgs = append(dockerArgs, args...)

	res, err := c.r.Exec(c.ctx, ExecSpec{
		Cmd:    "docker",
		Args:   dockerArgs,
		Prefix: "[exec]",
	})
	if err != nil || res.ExitCode != 0 {
		c.t.Fatalf("docker exec %s failed (exit=%d): %v", c.name, res.ExitCode, err)
	}
	return res.Output
}

// ExecBashOK runs a bash script inside the container.
func (c *TestContainer) ExecBashOK(env map[string]string, script string) string {
	c.t.Helper()

	return c.ExecEnvOK(env, "bash", "-lc", script)
}

// ExecBashOKTimeout runs a bash script with a custom timeout.
func (c *TestContainer) ExecBashOKTimeout(env map[string]string, script string, timeout time.Duration) string {
	c.t.Helper()

	dockerArgs := make([]string, 0, 2+len(env)*2+1+3)
	dockerArgs = append(dockerArgs, "exec")

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		dockerArgs = append(dockerArgs, "-e", k+"="+env[k])
	}

	dockerArgs = append(dockerArgs, c.name, "bash", "-lc", script)

	res, err := c.r.Exec(c.ctx, ExecSpec{
		Cmd:     "docker",
		Args:    dockerArgs,
		Prefix:  "[exec]",
		Timeout: timeout,
	})
	if err != nil || res.ExitCode != 0 {
		c.t.Fatalf("docker exec %s failed (exit=%d): %v", c.name, res.ExitCode, err)
	}
	return res.Output
}

// CopySourcesToWorkDir copies the mounted source directory to a work directory inside the container.
func (c *TestContainer) CopySourcesToWorkDir(workDir string) {
	c.t.Helper()

	c.ExecBashOK(nil, fmt.Sprintf("rm -rf %s && mkdir -p %s && cp -a /tmp/stevedore-src/. %s/", workDir, workDir, workDir))
}

// Restart stops and starts the container.
func (c *TestContainer) Restart() {
	c.t.Helper()

	c.docker.runOK("restart", c.name)
}

// Stop stops the container without removing it.
func (c *TestContainer) Stop() {
	c.t.Helper()

	c.docker.runOK("stop", c.name)
}

// Start starts a stopped container.
func (c *TestContainer) Start() {
	c.t.Helper()

	c.docker.runOK("start", c.name)
}

// dockerCLI is an internal helper for running docker commands.
type dockerCLI struct {
	t   testing.TB
	ctx context.Context
	r   *Runner
}

func (d *dockerCLI) run(args ...string) (ExecResult, error) {
	d.t.Helper()

	return d.r.Exec(d.ctx, ExecSpec{
		Cmd:    "docker",
		Args:   args,
		Prefix: "[docker]",
	})
}

func (d *dockerCLI) runOK(args ...string) string {
	d.t.Helper()

	res, err := d.run(args...)
	if err != nil || res.ExitCode != 0 {
		d.t.Fatalf("docker %s failed (exit=%d): %v", strings.Join(args, " "), res.ExitCode, err)
	}
	return res.Output
}

func (d *dockerCLI) stopAndRemoveContainer(name string) {
	d.t.Helper()
	if !d.containerExists(name) {
		return
	}
	_, _ = d.run("stop", name)
	_, _ = d.run("rm", "-f", name)
}

func (d *dockerCLI) removeImage(tag string) {
	d.t.Helper()
	if !d.imageExists(tag) {
		return
	}
	_, _ = d.run("rmi", "-f", tag)
}

func (d *dockerCLI) containerExists(name string) bool {
	d.t.Helper()
	res, err := d.runQuiet("ps", "-q", "--filter", "name=^"+name+"$")
	return err == nil && res.ExitCode == 0 && strings.TrimSpace(res.Output) != ""
}

func (d *dockerCLI) imageExists(tag string) bool {
	d.t.Helper()
	res, err := d.runQuiet("images", "-q", tag)
	return err == nil && res.ExitCode == 0 && strings.TrimSpace(res.Output) != ""
}

func (d *dockerCLI) runQuiet(args ...string) (ExecResult, error) {
	d.t.Helper()

	return d.r.Exec(d.ctx, ExecSpec{
		Cmd:  "docker",
		Args: args,
	})
}

func (d *dockerCLI) removeContainersByPrefix(prefix string) {
	d.t.Helper()

	res, err := d.run("ps", "-a", "--filter", "name="+prefix, "--format", "{{.Names}}")
	if err != nil {
		return
	}

	for _, name := range strings.Split(strings.ReplaceAll(res.Output, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if strings.HasPrefix(name, prefix) {
			d.stopAndRemoveContainer(name)
		}
	}
}

// testRepoRoot returns the absolute path to the repository root.
func testRepoRoot(t testing.TB) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine current file location")
	}

	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../.."))
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs repo root: %v", err)
	}
	return abs
}

// sanitizeDockerName converts a dockerfile name to a valid Docker image/container name component.
// Docker requires lowercase names and certain characters are not allowed.
// "Dockerfile.ubuntu" -> "ubuntu"
// "Dockerfile.debian-12" -> "debian-12"
func sanitizeDockerName(dockerfile string) string {
	name := strings.ToLower(dockerfile)
	name = strings.TrimPrefix(name, "dockerfile.")
	name = strings.TrimPrefix(name, "dockerfile-")
	name = strings.ReplaceAll(name, ".", "-")
	return name
}
