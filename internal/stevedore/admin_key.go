package stevedore

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// AdminKeyEnvVar is the environment variable for the admin key value.
	AdminKeyEnvVar = "STEVEDORE_ADMIN_KEY"
	// AdminKeyFileEnvVar is the environment variable for the admin key file path.
	AdminKeyFileEnvVar = "STEVEDORE_ADMIN_KEY_FILE"
	// AdminKeyFilename is the default filename for the admin key.
	AdminKeyFilename = "admin.key"
	// AdminKeyLength is the length of generated admin keys in bytes.
	AdminKeyLength = 32
)

// AdminKeyPath returns the default path to the admin key file.
func (i *Instance) AdminKeyPath() string {
	return filepath.Join(i.SystemDir(), AdminKeyFilename)
}

// GetAdminKey retrieves the admin key from environment or file.
// Priority order:
// 1. STEVEDORE_ADMIN_KEY environment variable
// 2. STEVEDORE_ADMIN_KEY_FILE environment variable (path to key file)
// 3. Default key file at system/admin.key
func (i *Instance) GetAdminKey() (string, error) {
	// Check environment variable first
	if key := strings.TrimSpace(os.Getenv(AdminKeyEnvVar)); key != "" {
		return key, nil
	}

	// Check environment variable for file path
	if keyFile := strings.TrimSpace(os.Getenv(AdminKeyFileEnvVar)); keyFile != "" {
		return readKeyFile(keyFile)
	}

	// Use default path
	return readKeyFile(i.AdminKeyPath())
}

// EnsureAdminKey generates an admin key if it doesn't exist.
func (i *Instance) EnsureAdminKey() error {
	keyPath := i.AdminKeyPath()

	// Check if key already exists
	if _, err := os.Stat(keyPath); err == nil {
		return nil
	}

	// Generate new key
	key, err := generateSecureKey(AdminKeyLength)
	if err != nil {
		return fmt.Errorf("generate admin key: %w", err)
	}

	// Write key with restricted permissions (owner read-only)
	if err := os.WriteFile(keyPath, []byte(key+"\n"), 0600); err != nil {
		return fmt.Errorf("write admin key: %w", err)
	}

	return nil
}

// ValidateAdminKey checks if the provided key matches the stored admin key.
func (i *Instance) ValidateAdminKey(providedKey string) (bool, error) {
	storedKey, err := i.GetAdminKey()
	if err != nil {
		return false, err
	}

	// Use constant-time comparison to prevent timing attacks
	return secureCompare(providedKey, storedKey), nil
}

// readKeyFile reads and returns the key from the specified file.
func readKeyFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read key file %s: %w", path, err)
	}

	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("key file %s is empty", path)
	}

	return key, nil
}

// generateSecureKey generates a cryptographically secure random key.
func generateSecureKey(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

// secureCompare performs a constant-time string comparison.
// This prevents timing attacks when comparing authentication tokens.
func secureCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}

	var result byte
	for i := 0; i < len(a); i++ {
		result |= a[i] ^ b[i]
	}

	return result == 0
}
