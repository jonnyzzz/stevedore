package stevedore

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mutecomm/go-sqlcipher/v4"
)

func (i *Instance) DBPath() string {
	return filepath.Join(i.SystemDir(), "stevedore.db")
}

func (i *Instance) DBKeyPath() string {
	return filepath.Join(i.SystemDir(), "db.key")
}

func (i *Instance) OpenDB() (*sql.DB, error) {
	if err := i.EnsureLayout(); err != nil {
		return nil, err
	}

	key, err := i.dbKey()
	if err != nil {
		return nil, err
	}

	if err := i.ensureDBFile(); err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("file:%s?_pragma_key=%s", i.DBPath(), url.QueryEscape(key))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)

	if err := i.configureDB(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrateDB(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func (i *Instance) ensureDBFile() error {
	path := i.DBPath()
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

func (i *Instance) dbKey() (string, error) {
	if v := strings.TrimSpace(os.Getenv("STEVEDORE_DB_KEY")); v != "" {
		return v, nil
	}

	if keyFile := strings.TrimSpace(os.Getenv("STEVEDORE_DB_KEY_FILE")); keyFile != "" {
		b, err := os.ReadFile(keyFile)
		if err != nil {
			return "", fmt.Errorf("read STEVEDORE_DB_KEY_FILE: %w", err)
		}
		key := strings.TrimSpace(string(b))
		if key == "" {
			return "", fmt.Errorf("empty STEVEDORE_DB_KEY_FILE: %s", keyFile)
		}
		return key, nil
	}

	b, err := os.ReadFile(i.DBKeyPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("database key is missing (expected %s); run ./stevedore-install.sh", i.DBKeyPath())
		}
		return "", err
	}

	key := strings.TrimSpace(string(b))
	if key == "" {
		return "", fmt.Errorf("database key is empty: %s", i.DBKeyPath())
	}

	return key, nil
}

func (i *Instance) configureDB(db *sql.DB) error {
	for _, stmt := range []string{
		"PRAGMA foreign_keys = ON;",
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA busy_timeout = 5000;",
	} {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDeploymentRow inserts a deployment row if it doesn't exist.
func EnsureDeploymentRow(db *sql.DB, deployment string) error {
	_, err := db.Exec(`INSERT INTO deployments (name) VALUES (?) ON CONFLICT(name) DO NOTHING;`, deployment)
	return err
}
