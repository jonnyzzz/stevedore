package stevedore

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"
)

// DaemonConfig holds configuration for the daemon.
type DaemonConfig struct {
	AdminKey     string
	ListenAddr   string
	Version      string
	MinPollTime  time.Duration // Minimum time between poll cycles (default: 30s)
	SyncTimeout  time.Duration // Timeout for sync operations (default: 5m)
	DeployTimeout time.Duration // Timeout for deploy operations (default: 10m)
}

// Daemon manages the polling loop and HTTP server.
type Daemon struct {
	instance *Instance
	db       *sql.DB
	config   DaemonConfig
	server   *Server
	mu       sync.Mutex
	syncing  map[string]bool // Track which deployments are currently syncing
}

// NewDaemon creates a new daemon instance.
func NewDaemon(instance *Instance, db *sql.DB, config DaemonConfig) *Daemon {
	if config.ListenAddr == "" {
		config.ListenAddr = ":42107"
	}
	if config.MinPollTime == 0 {
		config.MinPollTime = 30 * time.Second
	}
	if config.SyncTimeout == 0 {
		config.SyncTimeout = 5 * time.Minute
	}
	if config.DeployTimeout == 0 {
		config.DeployTimeout = 10 * time.Minute
	}

	d := &Daemon{
		instance: instance,
		db:       db,
		config:   config,
		syncing:  make(map[string]bool),
	}

	d.server = NewServer(instance, db, ServerConfig{
		AdminKey:   config.AdminKey,
		ListenAddr: config.ListenAddr,
	}, config.Version)

	return d
}

// Run starts the daemon and blocks until context is canceled.
func (d *Daemon) Run(ctx context.Context) error {
	// Start HTTP server
	if err := d.server.Start(); err != nil {
		return err
	}

	// Run polling loop
	d.runPollLoop(ctx)

	// Shutdown HTTP server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return d.server.Shutdown(shutdownCtx)
}

// runPollLoop runs the main polling loop.
func (d *Daemon) runPollLoop(ctx context.Context) {
	// Use a shorter ticker for checking; actual polls are gated by per-deployment intervals
	ticker := time.NewTicker(d.config.MinPollTime)
	defer ticker.Stop()

	// Run an initial poll immediately
	d.pollAllDeployments(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Polling loop stopping")
			return
		case <-ticker.C:
			d.pollAllDeployments(ctx)
		}
	}
}

// pollAllDeployments polls all enabled deployments that are due for sync.
func (d *Daemon) pollAllDeployments(ctx context.Context) {
	deployments, err := d.instance.ListEnabledDeployments(d.db)
	if err != nil {
		log.Printf("Error listing deployments: %v", err)
		return
	}

	now := time.Now()

	for _, deployment := range deployments {
		// Check if deployment is due for sync
		syncStatus, err := d.instance.GetSyncStatus(d.db, deployment.Deployment)
		if err != nil {
			log.Printf("Error getting sync status for %s: %v", deployment.Deployment, err)
			continue
		}

		// Calculate next sync time
		pollInterval := time.Duration(deployment.PollIntervalSeconds) * time.Second
		nextSync := syncStatus.LastSyncAt.Add(pollInterval)

		if now.Before(nextSync) {
			// Not due yet
			continue
		}

		// Check if already syncing
		if d.isAlreadySyncing(deployment.Deployment) {
			continue
		}

		// Sync in a goroutine to avoid blocking other deployments
		go d.syncDeployment(ctx, deployment.Deployment)
	}
}

// isAlreadySyncing checks if a deployment is currently being synced.
func (d *Daemon) isAlreadySyncing(deployment string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.syncing[deployment]
}

// setSyncing marks a deployment as syncing or not.
func (d *Daemon) setSyncing(deployment string, syncing bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if syncing {
		d.syncing[deployment] = true
	} else {
		delete(d.syncing, deployment)
	}
}

// syncDeployment performs sync and optional deploy for a single deployment.
func (d *Daemon) syncDeployment(parentCtx context.Context, deployment string) {
	d.setSyncing(deployment, true)
	defer d.setSyncing(deployment, false)

	log.Printf("Syncing deployment: %s", deployment)

	// Get current sync status to compare commits
	syncStatus, err := d.instance.GetSyncStatus(d.db, deployment)
	if err != nil {
		log.Printf("Error getting sync status for %s: %v", deployment, err)
	}
	previousCommit := ""
	if syncStatus != nil {
		previousCommit = syncStatus.LastCommit
	}

	// Sync with timeout
	syncCtx, syncCancel := context.WithTimeout(parentCtx, d.config.SyncTimeout)
	defer syncCancel()

	result, err := d.instance.GitCloneLocal(syncCtx, deployment)
	if err != nil {
		log.Printf("Sync failed for %s: %v", deployment, err)
		_ = d.instance.UpdateSyncError(d.db, deployment, err)
		return
	}

	// Update sync status
	if err := d.instance.UpdateSyncStatus(d.db, deployment, result.Commit); err != nil {
		log.Printf("Warning: failed to update sync status for %s: %v", deployment, err)
	}

	log.Printf("Synced %s: %s@%s", deployment, result.Branch, shortCommit(result.Commit))

	// Check if commit changed
	if previousCommit != "" && previousCommit == result.Commit {
		// No change, skip deploy
		return
	}

	// New commit detected, deploy
	log.Printf("New commit detected for %s (was: %s, now: %s), deploying...",
		deployment, shortCommit(previousCommit), shortCommit(result.Commit))

	// Check for self-update
	if deployment == "stevedore" {
		log.Printf("Self-update detected for stevedore deployment - skipping auto-deploy")
		log.Printf("Run self-update manually or restart the daemon to apply changes")
		return
	}

	// Deploy with timeout
	deployCtx, deployCancel := context.WithTimeout(parentCtx, d.config.DeployTimeout)
	defer deployCancel()

	deployResult, err := d.instance.Deploy(deployCtx, deployment, ComposeConfig{})
	if err != nil {
		log.Printf("Deploy failed for %s: %v", deployment, err)
		return
	}

	// Update deploy status
	if err := d.instance.UpdateDeployStatus(d.db, deployment); err != nil {
		log.Printf("Warning: failed to update deploy status for %s: %v", deployment, err)
	}

	log.Printf("Deployed %s: project=%s, services=%v",
		deployment, deployResult.ProjectName, deployResult.Services)
}

// shortCommit returns the first 12 characters of a commit hash.
func shortCommit(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

// TriggerSync manually triggers a sync for a deployment.
// This is called by the HTTP API.
func (d *Daemon) TriggerSync(ctx context.Context, deployment string) error {
	if d.isAlreadySyncing(deployment) {
		return nil // Already syncing
	}

	go d.syncDeployment(ctx, deployment)
	return nil
}
