package stevedore

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
)

func (i *Instance) SetParameter(deployment string, name string, value []byte) error {
	if err := ValidateDeploymentName(deployment); err != nil {
		return err
	}
	if err := ValidateParameterName(name); err != nil {
		return err
	}
	if err := i.EnsureLayout(); err != nil {
		return err
	}

	if _, err := os.Stat(i.DeploymentDir(deployment)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("deployment not found: %s (run: stevedore repo add ...)", deployment)
		}
		return err
	}

	db, err := i.OpenDB()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := EnsureDeploymentRow(db, deployment); err != nil {
		return err
	}

	_, err = db.Exec(
		`INSERT INTO parameters (deployment, name, value, updated_at)
		 VALUES (?, ?, ?, CAST(strftime('%s','now') AS INTEGER))
		 ON CONFLICT(deployment, name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at;`,
		deployment,
		name,
		value,
	)
	return err
}

func (i *Instance) GetParameter(deployment string, name string) ([]byte, error) {
	if err := ValidateDeploymentName(deployment); err != nil {
		return nil, err
	}
	if err := ValidateParameterName(name); err != nil {
		return nil, err
	}
	if err := i.EnsureLayout(); err != nil {
		return nil, err
	}

	if _, err := os.Stat(i.DeploymentDir(deployment)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("deployment not found: %s (run: stevedore repo add ...)", deployment)
		}
		return nil, err
	}

	db, err := i.OpenDB()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	var value []byte
	if err := db.QueryRow(`SELECT value FROM parameters WHERE deployment = ? AND name = ?;`, deployment, name).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("parameter not found: %s/%s", deployment, name)
		}
		return nil, err
	}

	return value, nil
}

func (i *Instance) ListParameters(deployment string) ([]string, error) {
	if err := ValidateDeploymentName(deployment); err != nil {
		return nil, err
	}

	if _, err := os.Stat(i.DeploymentDir(deployment)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("deployment not found: %s (run: stevedore repo add ...)", deployment)
		}
		return nil, err
	}

	db, err := i.OpenDB()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(`SELECT name FROM parameters WHERE deployment = ? ORDER BY name;`, deployment)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return names, nil
}
