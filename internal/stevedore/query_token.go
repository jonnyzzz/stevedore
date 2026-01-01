package stevedore

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

// QueryTokenLength is the length of generated query tokens in bytes.
const QueryTokenLength = 32

// GenerateQueryToken generates a cryptographically secure random token.
func GenerateQueryToken() (string, error) {
	bytes := make([]byte, QueryTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// EnsureQueryToken ensures a query token exists for the deployment.
// If no token exists, one is generated and stored.
// Returns the token (existing or newly created).
func (i *Instance) EnsureQueryToken(deployment string) (string, error) {
	if err := ValidateDeploymentName(deployment); err != nil {
		return "", err
	}

	db, err := i.OpenDB()
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	// Check if token already exists
	var existingToken string
	err = db.QueryRow(`SELECT token FROM query_tokens WHERE deployment = ?;`, deployment).Scan(&existingToken)
	if err == nil {
		return existingToken, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("failed to check existing token: %w", err)
	}

	// Ensure deployment row exists
	if err := EnsureDeploymentRow(db, deployment); err != nil {
		return "", err
	}

	// Generate new token
	token, err := GenerateQueryToken()
	if err != nil {
		return "", err
	}

	// Store the token
	_, err = db.Exec(
		`INSERT INTO query_tokens (deployment, token) VALUES (?, ?);`,
		deployment, token,
	)
	if err != nil {
		return "", fmt.Errorf("failed to store query token: %w", err)
	}

	return token, nil
}

// GetQueryToken retrieves the query token for a deployment.
// Returns an error if no token exists.
func (i *Instance) GetQueryToken(deployment string) (string, error) {
	if err := ValidateDeploymentName(deployment); err != nil {
		return "", err
	}

	db, err := i.OpenDB()
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	var token string
	err = db.QueryRow(`SELECT token FROM query_tokens WHERE deployment = ?;`, deployment).Scan(&token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("no query token for deployment: %s", deployment)
		}
		return "", err
	}

	return token, nil
}

// ValidateQueryToken validates a token and returns the deployment it belongs to.
// Returns an error if the token is invalid.
func (i *Instance) ValidateQueryToken(token string) (string, error) {
	if token == "" {
		return "", errors.New("empty token")
	}

	db, err := i.OpenDB()
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	var deployment string
	err = db.QueryRow(`SELECT deployment FROM query_tokens WHERE token = ?;`, token).Scan(&deployment)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("invalid token")
		}
		return "", err
	}

	return deployment, nil
}

// RegenerateQueryToken generates a new token for the deployment, replacing any existing one.
func (i *Instance) RegenerateQueryToken(deployment string) (string, error) {
	if err := ValidateDeploymentName(deployment); err != nil {
		return "", err
	}

	db, err := i.OpenDB()
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	// Ensure deployment row exists
	if err := EnsureDeploymentRow(db, deployment); err != nil {
		return "", err
	}

	// Generate new token
	token, err := GenerateQueryToken()
	if err != nil {
		return "", err
	}

	// Upsert the token
	_, err = db.Exec(
		`INSERT INTO query_tokens (deployment, token) VALUES (?, ?)
		 ON CONFLICT(deployment) DO UPDATE SET token = excluded.token, created_at = CAST(strftime('%s','now') AS INTEGER);`,
		deployment, token,
	)
	if err != nil {
		return "", fmt.Errorf("failed to store query token: %w", err)
	}

	return token, nil
}

// ListQueryTokens returns all deployments with query tokens.
func (i *Instance) ListQueryTokens() (map[string]string, error) {
	db, err := i.OpenDB()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(`SELECT deployment, token FROM query_tokens ORDER BY deployment;`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	tokens := make(map[string]string)
	for rows.Next() {
		var deployment, token string
		if err := rows.Scan(&deployment, &token); err != nil {
			return nil, err
		}
		tokens[deployment] = token
	}

	return tokens, rows.Err()
}
