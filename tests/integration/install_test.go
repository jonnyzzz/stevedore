//go:build integration
// +build integration

package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInstaller_UbuntuDonorContainer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	docker := NewDockerCLI(t, ctx)
	if !docker.HasDocker() {
		t.Skip("docker is not installed")
	}

	repoRoot := repoRoot(t)

	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	containerPrefix := "stevedore-it-" + runID
	donorName := containerPrefix + "-donor"
	stevedoreContainerName := containerPrefix + "-stevedore"
	imageTag := "stevedore:it-" + runID

	stateRoot := filepath.Join(repoRoot, ".tmp", "install-"+runID)
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		t.Fatalf("mkdir state root: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(stateRoot) })
	t.Cleanup(func() { docker.RemoveImage(imageTag) })
	t.Cleanup(func() { docker.RemoveContainer(stevedoreContainerName) })
	t.Cleanup(func() { docker.RemoveContainer(donorName) })

	docker.RemoveContainersByPrefix("stevedore-it-")

	repoRootMount := repoRoot + ":/tmp/stevedore-src:ro"
	stateRootMount := stateRoot + ":" + stateRoot

	docker.RunOK(
		"run",
		"-d",
		"--name",
		donorName,
		"-v",
		"/var/run/docker.sock:/var/run/docker.sock",
		"-v",
		repoRootMount,
		"-v",
		stateRootMount,
		"ubuntu:22.04",
		"sleep",
		"infinity",
	)

	donor := docker.Container(donorName)

	donor.ExecBashOKTimeout(
		map[string]string{"DEBIAN_FRONTEND": "noninteractive"},
		"apt-get update -qq && apt-get install -y --no-install-recommends -qq ca-certificates git docker.io",
		10*time.Minute,
	)

	donor.ExecBashOK(
		nil,
		"rm -rf /work/stevedore && mkdir -p /work/stevedore && cp -a /tmp/stevedore-src/. /work/stevedore/",
	)
	donor.ExecOK("test", "-x", "/work/stevedore/stevedore-install.sh")

	installEnv := map[string]string{
		"STEVEDORE_ALLOW_UPSTREAM_MAIN": "1",
		"STEVEDORE_ASSUME_YES":          "1",
		"STEVEDORE_BOOTSTRAP_SELF":      "0",
		"STEVEDORE_CONTAINER_NAME":      stevedoreContainerName,
		"STEVEDORE_HOST_ROOT":           stateRoot,
		"STEVEDORE_IMAGE":               imageTag,
	}
	donor.ExecBashOKTimeout(installEnv, "cd /work/stevedore && ./stevedore-install.sh", 20*time.Minute)

	dbKeyPath := filepath.Join(stateRoot, "system", "db.key")
	containerEnvPath := filepath.Join(stateRoot, "system", "container.env")
	donor.ExecOK("test", "-s", dbKeyPath)
	donor.ExecOK("test", "-f", containerEnvPath)

	runningNames := donor.ExecOK("docker", "ps", "--format", "{{.Names}}")
	if !containsLine(runningNames, stevedoreContainerName) {
		t.Fatalf("expected stevedore container to be running: %s", stevedoreContainerName)
	}

	restartPolicy := strings.TrimSpace(donor.ExecOK("docker", "inspect", "-f", "{{.HostConfig.RestartPolicy.Name}}", stevedoreContainerName))
	if restartPolicy != "unless-stopped" {
		t.Fatalf("unexpected restart policy: %q", restartPolicy)
	}

	imageUsed := strings.TrimSpace(donor.ExecOK("docker", "inspect", "-f", "{{.Config.Image}}", stevedoreContainerName))
	if imageUsed != imageTag {
		t.Fatalf("unexpected stevedore image: %q (want %q)", imageUsed, imageTag)
	}

	donor.ExecOK("test", "-x", "/usr/local/bin/stevedore")

	wrapperEnv := map[string]string{"STEVEDORE_CONTAINER": stevedoreContainerName}
	doctorOut := donor.ExecEnvOK(wrapperEnv, "stevedore", "doctor")
	if !strings.Contains(doctorOut, "deployments:") {
		t.Fatalf("unexpected doctor output: %q", doctorOut)
	}

	expectedVersion := strings.TrimSpace(donor.ExecOK("cat", "/work/stevedore/VERSION"))
	versionOut := strings.TrimSpace(donor.ExecEnvOK(wrapperEnv, "stevedore", "version"))
	if !strings.HasPrefix(versionOut, "stevedore "+expectedVersion) {
		t.Fatalf("unexpected version output: %q", versionOut)
	}
	directVersionOut := strings.TrimSpace(donor.ExecOK("docker", "exec", "-i", stevedoreContainerName, "/app/stevedore", "version"))
	if directVersionOut != versionOut {
		t.Fatalf("wrapper version differs from direct exec: wrapper=%q direct=%q", versionOut, directVersionOut)
	}
	if strings.Contains(versionOut, "unknown") {
		t.Fatalf("version output contains 'unknown': %q", versionOut)
	}
	if strings.Contains(versionOut, "://") {
		t.Fatalf("version output contains a URL: %q", versionOut)
	}

	repoAddOut := donor.ExecEnvOK(wrapperEnv, "stevedore", "repo", "add", "demo", "git@github.com:acme/demo.git", "--branch", "main")
	if !strings.Contains(repoAddOut, "ssh-ed25519") {
		t.Fatalf("repo add did not output a public key: %q", repoAddOut)
	}
	pubKey := strings.TrimSpace(donor.ExecEnvOK(wrapperEnv, "stevedore", "repo", "key", "demo"))
	if !strings.HasPrefix(pubKey, "ssh-ed25519 ") {
		t.Fatalf("unexpected public key: %q", pubKey)
	}

	repoList := donor.ExecEnvOK(wrapperEnv, "stevedore", "repo", "list")
	if !containsLine(repoList, "demo") {
		t.Fatalf("expected demo deployment in repo list output: %q", repoList)
	}

	donor.ExecEnvOK(wrapperEnv, "stevedore", "param", "set", "demo", "DEMO_KEY", "demo-value")
	value := strings.TrimSpace(donor.ExecEnvOK(wrapperEnv, "stevedore", "param", "get", "demo", "DEMO_KEY"))
	if value != "demo-value" {
		t.Fatalf("unexpected parameter value: %q", value)
	}

	dbPath := filepath.Join(stateRoot, "system", "stevedore.db")
	header, err := readFileHeader(dbPath, 16)
	if err != nil {
		t.Fatalf("read db header: %v", err)
	}
	if bytes.Equal(header, []byte("SQLite format 3\x00")) {
		t.Fatalf("database file looks unencrypted (SQLite header detected): %s", dbPath)
	}

	wrongKeyRes, err := donor.Exec("docker", "exec", "-e", "STEVEDORE_DB_KEY=wrong", stevedoreContainerName, "/app/stevedore", "param", "get", "demo", "DEMO_KEY")
	if err == nil && wrongKeyRes.ExitCode == 0 {
		t.Fatalf("expected wrong DB key to fail, but command succeeded")
	}

	legacyParamFile := filepath.Join(stateRoot, "deployments", "demo", "parameters", "DEMO_KEY.txt")
	res, err := donor.Exec("test", "!", "-f", legacyParamFile)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("legacy parameter file exists (should not): %s", legacyParamFile)
	}
}

func repoRoot(t testing.TB) string {
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

func readFileHeader(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, n)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
