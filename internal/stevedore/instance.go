package stevedore

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

const DefaultRoot = "/opt/stevedore"

var deploymentNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
var parameterNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Instance struct {
	Root string
}

func NewInstance(root string) *Instance {
	if root == "" {
		root = DefaultRoot
	}
	return &Instance{Root: root}
}

func (i *Instance) EnsureLayout() error {
	if err := os.MkdirAll(i.SystemDir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(i.DeploymentsDir(), 0o755); err != nil {
		return err
	}
	return nil
}

func (i *Instance) SystemDir() string {
	return filepath.Join(i.Root, "system")
}

func (i *Instance) DeploymentsDir() string {
	return filepath.Join(i.Root, "deployments")
}

func (i *Instance) DeploymentDir(name string) string {
	return filepath.Join(i.DeploymentsDir(), name)
}

func (i *Instance) ListDeployments() ([]string, error) {
	entries, err := os.ReadDir(i.DeploymentsDir())
	if err != nil {
		return nil, err
	}

	var deployments []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		deployments = append(deployments, e.Name())
	}

	sort.Strings(deployments)
	return deployments, nil
}

func ValidateDeploymentName(name string) error {
	if !deploymentNameRe.MatchString(name) {
		return fmt.Errorf("invalid deployment name: %q", name)
	}
	return nil
}

func ValidateParameterName(name string) error {
	if !parameterNameRe.MatchString(name) {
		return fmt.Errorf("invalid parameter name: %q", name)
	}
	return nil
}
