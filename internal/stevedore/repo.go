package stevedore

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type RepoSpec struct {
	URL    string
	Branch string
}

func (i *Instance) AddRepo(deployment string, spec RepoSpec) (string, error) {
	if err := ValidateDeploymentName(deployment); err != nil {
		return "", err
	}
	if spec.URL == "" {
		return "", fmt.Errorf("repo url is required")
	}
	if spec.Branch == "" {
		spec.Branch = "main"
	}
	if err := i.EnsureLayout(); err != nil {
		return "", err
	}

	deploymentDir := i.DeploymentDir(deployment)
	if _, err := os.Stat(deploymentDir); err == nil {
		return "", fmt.Errorf("deployment already exists: %s", deployment)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	repoDir := filepath.Join(deploymentDir, "repo")
	repoSSHDir := filepath.Join(repoDir, "ssh")
	repoGitDir := filepath.Join(repoDir, "git")
	parametersDir := filepath.Join(deploymentDir, "parameters")
	runtimeDir := filepath.Join(deploymentDir, "runtime")
	dataDir := filepath.Join(deploymentDir, "data")
	logsDir := filepath.Join(deploymentDir, "logs")

	for _, dir := range []string{repoSSHDir, repoGitDir, parametersDir, runtimeDir, dataDir, logsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}

	if err := writeFileAtomic(filepath.Join(repoDir, "url.txt"), []byte(spec.URL+"\n"), 0o644); err != nil {
		return "", err
	}
	if err := writeFileAtomic(filepath.Join(repoDir, "branch.txt"), []byte(spec.Branch+"\n"), 0o644); err != nil {
		return "", err
	}

	privateKeyPath := filepath.Join(repoSSHDir, "id_ed25519")
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "stevedore:"+deployment, "-f", privateKeyPath, "-q")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ssh-keygen failed: %w (%s)", err, strings.TrimSpace(out.String()))
	}

	db, err := i.OpenDB()
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	if err := EnsureDeploymentRow(db, deployment); err != nil {
		return "", err
	}
	if _, err := db.Exec(
		`INSERT INTO repositories (deployment, url, branch, updated_at)
		 VALUES (?, ?, ?, CAST(strftime('%s','now') AS INTEGER))
		 ON CONFLICT(deployment) DO UPDATE SET url = excluded.url, branch = excluded.branch, updated_at = excluded.updated_at;`,
		deployment,
		spec.URL,
		spec.Branch,
	); err != nil {
		return "", err
	}

	return i.RepoPublicKey(deployment)
}

func (i *Instance) RepoPublicKey(deployment string) (string, error) {
	if err := ValidateDeploymentName(deployment); err != nil {
		return "", err
	}

	publicKeyPath := filepath.Join(i.DeploymentDir(deployment), "repo", "ssh", "id_ed25519.pub")
	b, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(b)), nil
}
