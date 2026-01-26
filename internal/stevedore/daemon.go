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
	AdminKey          string
	ListenAddr        string
	Version           string
	Build             string        // Git commit or build hash for strict version matching
	MinPollTime       time.Duration // Minimum time between poll cycles (default: 30s)
	SyncTimeout       time.Duration // Timeout for sync operations (default: 5m)
	DeployTimeout     time.Duration // Timeout for deploy operations (default: 10m)
	ReconcileInterval time.Duration // Interval for reconcile checks (default: 30s)
	QuerySocketPath   string        // Path for query socket (default: /var/run/stevedore/query.sock)
}

// Daemon manages the polling loop and HTTP server.
type Daemon struct {
	instance    *Instance
	db          *sql.DB
	config      DaemonConfig
	server      *Server
	queryServer *QueryServer
	mu          sync.Mutex
	active      map[string]bool // Track deployments currently being processed
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
	if config.ReconcileInterval == 0 {
		config.ReconcileInterval = 30 * time.Second
	}
	if config.QuerySocketPath == "" {
		config.QuerySocketPath = DefaultQuerySocketPath
	}

	d := &Daemon{
		instance: instance,
		db:       db,
		config:   config,
		active:   make(map[string]bool),
	}

	d.server = NewServer(instance, db, ServerConfig{
		AdminKey:   config.AdminKey,
		ListenAddr: config.ListenAddr,
	}, config.Version, config.Build)

	d.queryServer = NewQueryServer(instance, config.QuerySocketPath)

	return d
}

// Run starts the daemon and blocks until context is canceled.
func (d *Daemon) Run(ctx context.Context) error {
	// Start HTTP server
	if err := d.server.Start(); err != nil {
		return err
	}

	// Start query socket server in background
	go func() {
		if err := d.queryServer.Start(ctx); err != nil {
			log.Printf("Query server error: %v", err)
		}
	}()

	// Start reconcile loop in background
	go d.runReconcileLoop(ctx)

	// Run polling loop
	d.runPollLoop(ctx)

	// Shutdown HTTP server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop query server
	_ = d.queryServer.Stop()

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

// runReconcileLoop periodically checks deployments and restarts stopped services.
func (d *Daemon) runReconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(d.config.ReconcileInterval)
	defer ticker.Stop()

	// Run an initial reconcile immediately
	d.reconcileAllDeployments(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Reconcile loop stopping")
			return
		case <-ticker.C:
			d.reconcileAllDeployments(ctx)
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
		if d.isActive(deployment.Deployment) {
			continue
		}

		// Sync in a goroutine to avoid blocking other deployments
		go d.syncDeployment(ctx, deployment.Deployment)
	}
}

// isActive checks if a deployment is currently being processed.
func (d *Daemon) isActive(deployment string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.active[deployment]
}

// setActive marks a deployment as active or not.
func (d *Daemon) setActive(deployment string, active bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if active {
		d.active[deployment] = true
	} else {
		delete(d.active, deployment)
	}
}

// syncDeployment performs check, sync, and optional deploy for a single deployment.
// It first checks for updates using git fetch only (safe while deployment runs),
// then syncs and deploys only if changes are detected.
func (d *Daemon) syncDeployment(parentCtx context.Context, deployment string) {
	d.setActive(deployment, true)
	defer d.setActive(deployment, false)

	config, err := d.instance.GetRepoConfig(d.db, deployment)
	if err != nil {
		log.Printf("Error loading repo config for %s: %v", deployment, err)
		return
	}
	if !config.Enabled {
		log.Printf("Skipping sync for disabled deployment: %s", deployment)
		return
	}

	log.Printf("Checking for updates: %s", deployment)

	// Step 1: Check for updates using git fetch only (doesn't modify working directory)
	checkCtx, checkCancel := context.WithTimeout(parentCtx, d.config.SyncTimeout)
	defer checkCancel()

	checkResult, err := d.instance.GitCheckRemote(checkCtx, deployment)
	if err != nil {
		log.Printf("Check failed for %s: %v", deployment, err)
		_ = d.instance.UpdateSyncError(d.db, deployment, err)
		return
	}

	// Update sync status with check time (even if no changes)
	if err := d.instance.UpdateSyncStatus(d.db, deployment, checkResult.CurrentCommit); err != nil {
		log.Printf("Warning: failed to update sync status for %s: %v", deployment, err)
	}

	if !checkResult.HasChanges {
		log.Printf("No updates for %s: %s@%s", deployment, checkResult.Branch, shortCommit(checkResult.CurrentCommit))
		return
	}

	// Step 2: Changes detected - sync the repository (with stale file cleanup)
	log.Printf("Updates available for %s (current: %s, remote: %s), syncing...",
		deployment, shortCommit(checkResult.CurrentCommit), shortCommit(checkResult.RemoteCommit))

	syncCtx, syncCancel := context.WithTimeout(parentCtx, d.config.SyncTimeout)
	defer syncCancel()

	// Use GitSyncClean to sync with stale file removal enabled by default
	result, err := d.instance.GitSyncClean(syncCtx, deployment, true)
	if err != nil {
		log.Printf("Sync failed for %s: %v", deployment, err)
		_ = d.instance.UpdateSyncError(d.db, deployment, err)
		return
	}

	// Update sync status with new commit
	if err := d.instance.UpdateSyncStatus(d.db, deployment, result.Commit); err != nil {
		log.Printf("Warning: failed to update sync status for %s: %v", deployment, err)
	}

	log.Printf("Synced %s: %s@%s", deployment, result.Branch, shortCommit(result.Commit))

	// Step 3: Deploy if this is not a self-update
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

	// Notify query server of deployment change
	d.queryServer.NotifyChange()
}

// reconcileAllDeployments checks deployments and restarts stopped services.
func (d *Daemon) reconcileAllDeployments(ctx context.Context) {
	deployments, err := d.instance.ListEnabledDeployments(d.db)
	if err != nil {
		log.Printf("Error listing deployments for reconcile: %v", err)
		return
	}

	for _, deployment := range deployments {
		if deployment.Deployment == "stevedore" {
			continue
		}
		if d.isActive(deployment.Deployment) {
			continue
		}
		go d.reconcileDeployment(ctx, deployment.Deployment)
	}
}

// reconcileDeployment ensures a deployment is running if it was previously deployed.
func (d *Daemon) reconcileDeployment(parentCtx context.Context, deployment string) {
	d.setActive(deployment, true)
	defer d.setActive(deployment, false)

	config, err := d.instance.GetRepoConfig(d.db, deployment)
	if err != nil {
		log.Printf("Error loading repo config for %s: %v", deployment, err)
		return
	}
	if !config.Enabled {
		return
	}

	syncStatus, err := d.instance.GetSyncStatus(d.db, deployment)
	if err != nil {
		log.Printf("Error getting sync status for %s: %v", deployment, err)
		return
	}
	if syncStatus.LastDeployAt.IsZero() {
		return
	}

	status, err := d.instance.GetDeploymentStatus(parentCtx, deployment)
	if err != nil {
		log.Printf("Error getting deployment status for %s: %v", deployment, err)
		return
	}
	if !needsReconcile(status) {
		return
	}

	config, err = d.instance.GetRepoConfig(d.db, deployment)
	if err != nil {
		log.Printf("Error loading repo config for %s: %v", deployment, err)
		return
	}
	if !config.Enabled {
		return
	}

	log.Printf("Reconcile: deployment %s not running (%s), restarting...", deployment, status.Message)

	deployCtx, deployCancel := context.WithTimeout(parentCtx, d.config.DeployTimeout)
	defer deployCancel()

	deployResult, err := d.instance.Deploy(deployCtx, deployment, ComposeConfig{})
	if err != nil {
		log.Printf("Reconcile deploy failed for %s: %v", deployment, err)
		_ = d.instance.UpdateSyncError(d.db, deployment, err)
		return
	}

	if err := d.instance.UpdateDeployStatus(d.db, deployment); err != nil {
		log.Printf("Warning: failed to update deploy status for %s: %v", deployment, err)
	}

	log.Printf("Reconciled %s: project=%s, services=%v",
		deployment, deployResult.ProjectName, deployResult.Services)

	d.queryServer.NotifyChange()
}

func needsReconcile(status *DeploymentStatus) bool {
	if status == nil {
		return false
	}
	if len(status.Containers) == 0 {
		return true
	}

	running := 0
	for _, c := range status.Containers {
		if c.State.IsStopped() {
			return true
		}
		if c.State == StateRunning || c.State == StateRestarting {
			running++
		}
	}

	return running == 0
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
	if d.isActive(deployment) {
		return nil // Already syncing
	}

	go d.syncDeployment(ctx, deployment)
	return nil
}

// SetExecutor sets the command executor for the HTTP API /api/exec endpoint.
func (d *Daemon) SetExecutor(executor CommandExecutor) {
	d.server.SetExecutor(executor)
}
