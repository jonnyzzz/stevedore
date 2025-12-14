package stevedore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var composeEntrypointCandidates = []string{
	"docker-compose.yaml",
	"docker-compose.yml",
	"compose.yaml",
	"compose.yml",
	"stevedore.yaml",
}

func FindComposeEntrypoint(repoRoot string) (string, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return "", errors.New("repoRoot is required")
	}

	for _, name := range composeEntrypointCandidates {
		path := filepath.Join(repoRoot, name)
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() {
				continue
			}
			return path, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}

	return "", fmt.Errorf("no compose entrypoint found (expected one of: %s)", strings.Join(composeEntrypointCandidates, ", "))
}
