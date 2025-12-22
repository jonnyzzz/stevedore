package stevedore

import (
	"database/sql"
	"fmt"
)

// Migration represents a database migration with a version and SQL statements.
type Migration struct {
	Version     int
	Description string
	Up          string
}

// Migrations is the ordered list of all database migrations.
// New migrations must be appended to the end with incrementing version numbers.
// Never modify existing migrations - always add new ones.
var Migrations = []Migration{
	{
		Version:     1,
		Description: "Initial schema: deployments, repositories, parameters tables",
		Up: `
CREATE TABLE IF NOT EXISTS deployments (
	name TEXT PRIMARY KEY,
	created_at INTEGER NOT NULL DEFAULT (CAST(strftime('%s','now') AS INTEGER))
);

CREATE TABLE IF NOT EXISTS repositories (
	deployment TEXT PRIMARY KEY,
	url TEXT NOT NULL,
	branch TEXT NOT NULL,
	updated_at INTEGER NOT NULL DEFAULT (CAST(strftime('%s','now') AS INTEGER)),
	FOREIGN KEY (deployment) REFERENCES deployments(name) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS parameters (
	deployment TEXT NOT NULL,
	name TEXT NOT NULL,
	value BLOB NOT NULL,
	updated_at INTEGER NOT NULL DEFAULT (CAST(strftime('%s','now') AS INTEGER)),
	PRIMARY KEY (deployment, name),
	FOREIGN KEY (deployment) REFERENCES deployments(name) ON DELETE CASCADE
);
`,
	},
	{
		Version:     2,
		Description: "Add sync status tracking and poll configuration",
		Up: `
CREATE TABLE IF NOT EXISTS sync_status (
	deployment TEXT PRIMARY KEY,
	last_commit TEXT,
	last_sync_at INTEGER,
	last_deploy_at INTEGER,
	last_error TEXT,
	last_error_at INTEGER,
	FOREIGN KEY (deployment) REFERENCES deployments(name) ON DELETE CASCADE
);
`,
	},
	{
		Version:     3,
		Description: "Add poll interval and enabled flag to repositories",
		Up: `
ALTER TABLE repositories ADD COLUMN poll_interval_seconds INTEGER NOT NULL DEFAULT 300;
ALTER TABLE repositories ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1;
`,
	},
}

// CurrentSchemaVersion returns the latest migration version.
func CurrentSchemaVersion() int {
	if len(Migrations) == 0 {
		return 0
	}
	return Migrations[len(Migrations)-1].Version
}

// migrateDB applies all pending migrations to the database.
func migrateDB(db *sql.DB) error {
	// Create schema_migrations table to track applied migrations
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at INTEGER NOT NULL DEFAULT (CAST(strftime('%s','now') AS INTEGER))
		);
	`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	// Get current schema version
	var currentVersion int
	err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations;`).Scan(&currentVersion)
	if err != nil {
		return fmt.Errorf("get current schema version: %w", err)
	}

	// Apply pending migrations
	for _, m := range Migrations {
		if m.Version <= currentVersion {
			continue
		}

		// Run migration in a transaction
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin transaction for migration %d: %w", m.Version, err)
		}

		if _, err := tx.Exec(m.Up); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d (%s): %w", m.Version, m.Description, err)
		}

		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version, description) VALUES (?, ?);`,
			m.Version, m.Description,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.Version, err)
		}
	}

	return nil
}

// GetSchemaVersion returns the current schema version from the database.
func GetSchemaVersion(db *sql.DB) (int, error) {
	var version int
	err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations;`).Scan(&version)
	if err != nil {
		return 0, err
	}
	return version, nil
}

// GetAppliedMigrations returns all applied migrations from the database.
func GetAppliedMigrations(db *sql.DB) ([]Migration, error) {
	rows, err := db.Query(`SELECT version, description FROM schema_migrations ORDER BY version;`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var migrations []Migration
	for rows.Next() {
		var m Migration
		if err := rows.Scan(&m.Version, &m.Description); err != nil {
			return nil, err
		}
		migrations = append(migrations, m)
	}
	return migrations, rows.Err()
}
