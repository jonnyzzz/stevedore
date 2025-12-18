package integration_test

import (
	"context"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"
)

type DockerCLI struct {
	t   testing.TB
	ctx context.Context
	r   *Runner
}

func NewDockerCLI(t testing.TB, ctx context.Context) *DockerCLI {
	t.Helper()
	return &DockerCLI{t: t, ctx: ctx, r: NewRunner(t)}
}

func (d *DockerCLI) HasDocker() bool {
	d.t.Helper()
	_, err := exec.LookPath("docker")
	return err == nil
}

func (d *DockerCLI) Run(args ...string) (ExecResult, error) {
	d.t.Helper()

	return d.r.Exec(d.ctx, ExecSpec{
		Cmd:    "docker",
		Args:   args,
		Prefix: "[docker]",
	})
}

func (d *DockerCLI) RunOK(args ...string) string {
	d.t.Helper()

	res, err := d.Run(args...)
	if err != nil || res.ExitCode != 0 {
		d.t.Fatalf("docker %s failed (exit=%d): %v", strings.Join(args, " "), res.ExitCode, err)
	}
	return res.Output
}

func (d *DockerCLI) Container(name string) *DockerContainer {
	d.t.Helper()
	return &DockerContainer{d: d, name: name}
}

func (d *DockerCLI) RemoveContainer(name string) {
	d.t.Helper()
	_, _ = d.Run("rm", "-f", name)
}

func (d *DockerCLI) RemoveImage(tag string) {
	d.t.Helper()
	_, _ = d.Run("rmi", "-f", tag)
}

func (d *DockerCLI) RemoveContainersByPrefix(prefix string) {
	d.t.Helper()

	out := d.RunOK("ps", "-a", "--filter", "name="+prefix, "--format", "{{.Names}}")
	for _, name := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if strings.HasPrefix(name, prefix) {
			d.RemoveContainer(name)
		}
	}
}

type DockerContainer struct {
	d    *DockerCLI
	name string
}

func (c *DockerContainer) Exec(args ...string) (ExecResult, error) {
	c.d.t.Helper()

	return c.d.r.Exec(c.d.ctx, ExecSpec{
		Cmd:    "docker",
		Args:   append([]string{"exec", c.name}, args...),
		Prefix: "[exec]",
	})
}

func (c *DockerContainer) ExecOK(args ...string) string {
	c.d.t.Helper()

	res, err := c.Exec(args...)
	if err != nil || res.ExitCode != 0 {
		c.d.t.Fatalf("docker exec %s %s failed (exit=%d): %v", c.name, strings.Join(args, " "), res.ExitCode, err)
	}
	return res.Output
}

func (c *DockerContainer) ExecEnvOK(env map[string]string, args ...string) string {
	c.d.t.Helper()

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

	res, err := c.d.r.Exec(c.d.ctx, ExecSpec{
		Cmd:    "docker",
		Args:   dockerArgs,
		Prefix: "[exec]",
	})
	if err != nil || res.ExitCode != 0 {
		c.d.t.Fatalf("docker exec %s failed (exit=%d): %v", c.name, res.ExitCode, err)
	}
	return res.Output
}

func (c *DockerContainer) ExecBashOK(env map[string]string, script string) string {
	c.d.t.Helper()

	return c.ExecEnvOK(env, "bash", "-lc", script)
}

func (c *DockerContainer) ExecBashOKTimeout(env map[string]string, script string, timeout time.Duration) string {
	c.d.t.Helper()

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

	res, err := c.d.r.Exec(c.d.ctx, ExecSpec{
		Cmd:     "docker",
		Args:    dockerArgs,
		Prefix:  "[exec]",
		Timeout: timeout,
	})
	if err != nil || res.ExitCode != 0 {
		c.d.t.Fatalf("docker exec %s failed (exit=%d): %v", c.name, res.ExitCode, err)
	}
	return res.Output
}
