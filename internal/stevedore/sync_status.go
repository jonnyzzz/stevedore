package stevedore

import (
	"database/sql"
	"errors"
	"time"
)

// SyncStatus represents the sync state of a deployment.
type SyncStatus struct {
	Deployment   string
	LastCommit   string
	LastSyncAt   time.Time
	LastDeployAt time.Time
	LastError    string
	LastErrorAt  time.Time
}

// GetSyncStatus retrieves the sync status for a deployment.
func (i *Instance) GetSyncStatus(db *sql.DB, deployment string) (*SyncStatus, error) {
	if err := ValidateDeploymentName(deployment); err != nil {
		return nil, err
	}

	var status SyncStatus
	var lastCommit, lastError sql.NullString
	var lastSyncAt, lastDeployAt, lastErrorAt sql.NullInt64

	err := db.QueryRow(`
		SELECT deployment, last_commit, last_sync_at, last_deploy_at, last_error, last_error_at
		FROM sync_status
		WHERE deployment = ?
	`, deployment).Scan(
		&status.Deployment,
		&lastCommit,
		&lastSyncAt,
		&lastDeployAt,
		&lastError,
		&lastErrorAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		// Return empty status for deployments without sync history
		return &SyncStatus{Deployment: deployment}, nil
	}
	if err != nil {
		return nil, err
	}

	if lastCommit.Valid {
		status.LastCommit = lastCommit.String
	}
	if lastSyncAt.Valid {
		status.LastSyncAt = time.Unix(lastSyncAt.Int64, 0)
	}
	if lastDeployAt.Valid {
		status.LastDeployAt = time.Unix(lastDeployAt.Int64, 0)
	}
	if lastError.Valid {
		status.LastError = lastError.String
	}
	if lastErrorAt.Valid {
		status.LastErrorAt = time.Unix(lastErrorAt.Int64, 0)
	}

	return &status, nil
}

// UpdateSyncStatus updates the sync status after a successful sync.
func (i *Instance) UpdateSyncStatus(db *sql.DB, deployment string, commit string) error {
	if err := ValidateDeploymentName(deployment); err != nil {
		return err
	}

	_, err := db.Exec(`
		INSERT INTO sync_status (deployment, last_commit, last_sync_at)
		VALUES (?, ?, CAST(strftime('%s','now') AS INTEGER))
		ON CONFLICT(deployment) DO UPDATE SET
			last_commit = excluded.last_commit,
			last_sync_at = excluded.last_sync_at,
			last_error = NULL,
			last_error_at = NULL
	`, deployment, commit)

	return err
}

// UpdateDeployStatus updates the deploy timestamp after a successful deploy.
func (i *Instance) UpdateDeployStatus(db *sql.DB, deployment string) error {
	if err := ValidateDeploymentName(deployment); err != nil {
		return err
	}

	_, err := db.Exec(`
		INSERT INTO sync_status (deployment, last_deploy_at)
		VALUES (?, CAST(strftime('%s','now') AS INTEGER))
		ON CONFLICT(deployment) DO UPDATE SET
			last_deploy_at = excluded.last_deploy_at
	`, deployment)

	return err
}

// UpdateSyncError records an error that occurred during sync.
func (i *Instance) UpdateSyncError(db *sql.DB, deployment string, syncErr error) error {
	if err := ValidateDeploymentName(deployment); err != nil {
		return err
	}

	errMsg := ""
	if syncErr != nil {
		errMsg = syncErr.Error()
	}

	_, err := db.Exec(`
		INSERT INTO sync_status (deployment, last_error, last_error_at)
		VALUES (?, ?, CAST(strftime('%s','now') AS INTEGER))
		ON CONFLICT(deployment) DO UPDATE SET
			last_error = excluded.last_error,
			last_error_at = excluded.last_error_at
	`, deployment, errMsg)

	return err
}

// RepoConfig holds repository configuration including poll settings.
type RepoConfig struct {
	Deployment          string
	URL                 string
	Branch              string
	PollIntervalSeconds int
	Enabled             bool
}

// GetRepoConfig retrieves repository configuration for a deployment.
func (i *Instance) GetRepoConfig(db *sql.DB, deployment string) (*RepoConfig, error) {
	if err := ValidateDeploymentName(deployment); err != nil {
		return nil, err
	}

	var config RepoConfig
	var enabled int

	err := db.QueryRow(`
		SELECT deployment, url, branch, poll_interval_seconds, enabled
		FROM repositories
		WHERE deployment = ?
	`, deployment).Scan(
		&config.Deployment,
		&config.URL,
		&config.Branch,
		&config.PollIntervalSeconds,
		&enabled,
	)

	if err != nil {
		return nil, err
	}

	config.Enabled = enabled != 0
	return &config, nil
}

// ListEnabledDeployments returns all enabled deployments with their poll intervals.
func (i *Instance) ListEnabledDeployments(db *sql.DB) ([]RepoConfig, error) {
	rows, err := db.Query(`
		SELECT deployment, url, branch, poll_interval_seconds, enabled
		FROM repositories
		WHERE enabled = 1
		ORDER BY deployment
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var configs []RepoConfig
	for rows.Next() {
		var config RepoConfig
		var enabled int
		if err := rows.Scan(
			&config.Deployment,
			&config.URL,
			&config.Branch,
			&config.PollIntervalSeconds,
			&enabled,
		); err != nil {
			return nil, err
		}
		config.Enabled = enabled != 0
		configs = append(configs, config)
	}

	return configs, rows.Err()
}

// SetDeploymentEnabled enables or disables a deployment for polling.
func (i *Instance) SetDeploymentEnabled(db *sql.DB, deployment string, enabled bool) error {
	if err := ValidateDeploymentName(deployment); err != nil {
		return err
	}

	enabledInt := 0
	if enabled {
		enabledInt = 1
	}

	_, err := db.Exec(`
		UPDATE repositories
		SET enabled = ?
		WHERE deployment = ?
	`, enabledInt, deployment)

	return err
}

// SetPollInterval sets the poll interval for a deployment in seconds.
func (i *Instance) SetPollInterval(db *sql.DB, deployment string, seconds int) error {
	if err := ValidateDeploymentName(deployment); err != nil {
		return err
	}

	if seconds < 60 {
		seconds = 60 // Minimum 1 minute
	}

	_, err := db.Exec(`
		UPDATE repositories
		SET poll_interval_seconds = ?
		WHERE deployment = ?
	`, seconds, deployment)

	return err
}
