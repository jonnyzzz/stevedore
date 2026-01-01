package stevedore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"
)

var namespaceRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// ValidateNamespace validates a shared config namespace name.
func ValidateNamespace(namespace string) error {
	if !namespaceRe.MatchString(namespace) {
		return fmt.Errorf("invalid namespace name: %q (must match %s)", namespace, namespaceRe.String())
	}
	return nil
}

// SharedDir returns the path to the shared configuration directory.
func (i *Instance) SharedDir() string {
	return filepath.Join(i.Root, "shared")
}

// sharedFilePath returns the path to a namespace's YAML file.
func (i *Instance) sharedFilePath(namespace string) string {
	return filepath.Join(i.SharedDir(), namespace+".yaml")
}

// EnsureSharedDir creates the shared directory if it doesn't exist.
func (i *Instance) EnsureSharedDir() error {
	return os.MkdirAll(i.SharedDir(), 0o755)
}

// ListSharedNamespaces returns a list of all shared config namespaces.
func (i *Instance) ListSharedNamespaces() ([]string, error) {
	entries, err := os.ReadDir(i.SharedDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var namespaces []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".yaml") {
			ns := strings.TrimSuffix(name, ".yaml")
			namespaces = append(namespaces, ns)
		}
	}

	sort.Strings(namespaces)
	return namespaces, nil
}

// ReadShared reads an entire namespace as a map.
func (i *Instance) ReadShared(namespace string) (map[string]interface{}, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return nil, err
	}

	path := i.sharedFilePath(namespace)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("namespace %q not found", namespace)
		}
		return nil, err
	}

	var result map[string]interface{}
	if err := yaml.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}

	if result == nil {
		result = make(map[string]interface{})
	}

	return result, nil
}

// ReadSharedKey reads a specific key from a namespace.
func (i *Instance) ReadSharedKey(namespace, key string) (interface{}, error) {
	data, err := i.ReadShared(namespace)
	if err != nil {
		return nil, err
	}

	value, ok := data[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found in namespace %q", key, namespace)
	}

	return value, nil
}

// WriteShared writes a key-value pair to a namespace with file locking.
func (i *Instance) WriteShared(namespace, key string, value interface{}) error {
	if err := ValidateNamespace(namespace); err != nil {
		return err
	}

	if err := i.EnsureSharedDir(); err != nil {
		return err
	}

	path := i.sharedFilePath(namespace)

	// Open or create the file for read-write
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer f.Close()

	// Acquire exclusive lock
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("failed to acquire lock on %s: %w", path, err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	// Read existing data
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	var existing map[string]interface{}
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("failed to parse existing %s: %w", path, err)
		}
	}
	if existing == nil {
		existing = make(map[string]interface{})
	}

	// Update the key
	existing[key] = value

	// Marshal back to YAML
	newData, err := yaml.Marshal(existing)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}

	// Truncate and write
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	if _, err := f.Write(newData); err != nil {
		return err
	}

	return nil
}

// ReadSharedRaw reads a namespace and returns the raw YAML string.
func (i *Instance) ReadSharedRaw(namespace string) (string, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return "", err
	}

	path := i.sharedFilePath(namespace)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("namespace %q not found", namespace)
		}
		return "", err
	}

	return string(data), nil
}
