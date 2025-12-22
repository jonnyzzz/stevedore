package stevedore

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// TestOpenDB_CreatesSchema tests that OpenDB creates the database with the correct schema.
func TestOpenDB_CreatesSchema(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Verify the database file was created
	if _, err := os.Stat(instance.DBPath()); err != nil {
		t.Fatalf("database file not created: %v", err)
	}

	// Verify the schema by querying table information
	tables := []string{"deployments", "repositories", "parameters", "schema_migrations"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?;`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

// TestMigrations_AppliesAll verifies all migrations are applied on fresh database.
func TestMigrations_AppliesAll(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	version, err := GetSchemaVersion(db)
	if err != nil {
		t.Fatalf("GetSchemaVersion: %v", err)
	}

	expectedVersion := CurrentSchemaVersion()
	if version != expectedVersion {
		t.Errorf("schema version = %d, want %d", version, expectedVersion)
	}
}

// TestMigrations_RecordsApplied verifies migration records are stored.
func TestMigrations_RecordsApplied(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	applied, err := GetAppliedMigrations(db)
	if err != nil {
		t.Fatalf("GetAppliedMigrations: %v", err)
	}

	if len(applied) != len(Migrations) {
		t.Errorf("applied migrations = %d, want %d", len(applied), len(Migrations))
	}

	for i, m := range applied {
		if m.Version != Migrations[i].Version {
			t.Errorf("migration[%d].Version = %d, want %d", i, m.Version, Migrations[i].Version)
		}
		if m.Description != Migrations[i].Description {
			t.Errorf("migration[%d].Description = %q, want %q", i, m.Description, Migrations[i].Description)
		}
	}
}

// TestMigrations_Idempotent verifies migrations can be run multiple times safely.
func TestMigrations_Idempotent(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	// First open - applies migrations
	db1, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB (first): %v", err)
	}
	_ = db1.Close()

	// Second open - should not fail, migrations already applied
	db2, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB (second): %v", err)
	}
	defer func() { _ = db2.Close() }()

	version, err := GetSchemaVersion(db2)
	if err != nil {
		t.Fatalf("GetSchemaVersion: %v", err)
	}

	if version != CurrentSchemaVersion() {
		t.Errorf("schema version = %d, want %d", version, CurrentSchemaVersion())
	}
}

// TestMigrations_SchemaDetails verifies the schema details of each table.
func TestMigrations_SchemaDetails(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Test deployments table columns
	t.Run("deployments", func(t *testing.T) {
		columns := getTableColumns(t, db, "deployments")
		expected := []string{"name", "created_at"}
		for _, col := range expected {
			if !columns[col] {
				t.Errorf("missing column %q in deployments", col)
			}
		}
	})

	// Test repositories table columns
	t.Run("repositories", func(t *testing.T) {
		columns := getTableColumns(t, db, "repositories")
		expected := []string{"deployment", "url", "branch", "updated_at"}
		for _, col := range expected {
			if !columns[col] {
				t.Errorf("missing column %q in repositories", col)
			}
		}
	})

	// Test parameters table columns
	t.Run("parameters", func(t *testing.T) {
		columns := getTableColumns(t, db, "parameters")
		expected := []string{"deployment", "name", "value", "updated_at"}
		for _, col := range expected {
			if !columns[col] {
				t.Errorf("missing column %q in parameters", col)
			}
		}
	})

	// Test schema_migrations table columns
	t.Run("schema_migrations", func(t *testing.T) {
		columns := getTableColumns(t, db, "schema_migrations")
		expected := []string{"version", "description", "applied_at"}
		for _, col := range expected {
			if !columns[col] {
				t.Errorf("missing column %q in schema_migrations", col)
			}
		}
	})
}

// TestEnsureDeploymentRow verifies the EnsureDeploymentRow function.
func TestEnsureDeploymentRow(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Insert a new deployment
	if err := EnsureDeploymentRow(db, "test-deployment"); err != nil {
		t.Fatalf("EnsureDeploymentRow: %v", err)
	}

	// Verify it exists
	var name string
	if err := db.QueryRow(`SELECT name FROM deployments WHERE name = ?;`, "test-deployment").Scan(&name); err != nil {
		t.Fatalf("query deployment: %v", err)
	}
	if name != "test-deployment" {
		t.Errorf("unexpected name: %q", name)
	}

	// Insert again (should not fail - ON CONFLICT DO NOTHING)
	if err := EnsureDeploymentRow(db, "test-deployment"); err != nil {
		t.Fatalf("EnsureDeploymentRow (second call): %v", err)
	}

	// Count should still be 1
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM deployments WHERE name = ?;`, "test-deployment").Scan(&count); err != nil {
		t.Fatalf("count deployments: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 deployment, got %d", count)
	}
}

// TestCurrentSchemaVersion verifies the version constant matches migrations.
func TestCurrentSchemaVersion(t *testing.T) {
	if len(Migrations) == 0 {
		t.Skip("no migrations defined")
	}

	version := CurrentSchemaVersion()
	lastMigration := Migrations[len(Migrations)-1]

	if version != lastMigration.Version {
		t.Errorf("CurrentSchemaVersion() = %d, want %d", version, lastMigration.Version)
	}
}

// TestMigrations_VersionsAreSequential verifies migration versions are sequential.
func TestMigrations_VersionsAreSequential(t *testing.T) {
	for i, m := range Migrations {
		expectedVersion := i + 1
		if m.Version != expectedVersion {
			t.Errorf("migration[%d].Version = %d, want %d", i, m.Version, expectedVersion)
		}
		if m.Description == "" {
			t.Errorf("migration[%d] has empty description", i)
		}
		if m.Up == "" {
			t.Errorf("migration[%d] has empty Up SQL", i)
		}
	}
}

// TestGenerateDBForTooling creates a database file in .db/ for IDE tooling inspection.
// Run with: go test -run TestGenerateDBForTooling -v ./internal/stevedore/
// Then configure IntelliJ Database tool with:
//   - Driver: SQLite (with SQLCipher support)
//   - File: .db/system/stevedore.db
//   - Password/Key: contents of .db/db.key
func TestGenerateDBForTooling(t *testing.T) {
	projectRoot := findProjectRoot(t)
	dbDir := filepath.Join(projectRoot, ".db")
	keyPath := filepath.Join(dbDir, "db.key")

	// Create .db directory
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("create .db dir: %v", err)
	}

	// Create a known key for tooling access
	const toolingKey = "stevedore-dev-key-for-tooling"
	if err := os.WriteFile(keyPath, []byte(toolingKey+"\n"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	// Create instance pointing to .db directory (uses standard layout: .db/system/stevedore.db)
	instance := NewInstance(dbDir)
	t.Setenv("STEVEDORE_DB_KEY", toolingKey)

	// Remove existing DB file for fresh start
	_ = os.Remove(instance.DBPath())

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Insert sample data for easier schema exploration
	if err := EnsureDeploymentRow(db, "example-deployment"); err != nil {
		t.Fatalf("insert sample deployment: %v", err)
	}

	_, err = db.Exec(`INSERT INTO repositories (deployment, url, branch) VALUES (?, ?, ?)
		ON CONFLICT(deployment) DO NOTHING;`,
		"example-deployment", "git@github.com:example/repo.git", "main")
	if err != nil {
		t.Fatalf("insert sample repository: %v", err)
	}

	_, err = db.Exec(`INSERT INTO parameters (deployment, name, value) VALUES (?, ?, ?)
		ON CONFLICT(deployment, name) DO NOTHING;`,
		"example-deployment", "EXAMPLE_PARAM", []byte("example-value"))
	if err != nil {
		t.Fatalf("insert sample parameter: %v", err)
	}

	t.Logf("Database created at: %s", instance.DBPath())
	t.Logf("Key file created at: %s", keyPath)
	t.Logf("Key value: %s", toolingKey)
	t.Logf("")
	t.Logf("IntelliJ setup:")
	t.Logf("  1. Database tool window -> + -> Data Source -> SQLite")
	t.Logf("  2. File: %s", instance.DBPath())
	t.Logf("  3. For SQLCipher, use password: %s", toolingKey)
}

func findProjectRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for {
		if _, err := os.Stat(dir + "/go.mod"); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find project root (go.mod)")
		}
		dir = parent
	}
}

func getTableColumns(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()

	rows, err := db.Query(`PRAGMA table_info(` + table + `);`)
	if err != nil {
		t.Fatalf("query table_info(%s): %v", table, err)
	}
	defer func() { _ = rows.Close() }()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt *string
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}
	return columns
}
