package stevedore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// HTTP headers for version verification between CLI and daemon
const (
	HeaderStevedoreVersion = "X-Stevedore-Version"
	HeaderStevedoreBuild   = "X-Stevedore-Build"
)

// ServerConfig holds configuration for the HTTP server.
type ServerConfig struct {
	AdminKey   string
	ListenAddr string
}

// CommandExecutor executes CLI commands inside the daemon process.
// This is set by main.go to provide access to the full CLI functionality.
type CommandExecutor func(args []string) (output string, exitCode int, err error)

// Server provides the HTTP API for Stevedore.
type Server struct {
	instance *Instance
	db       *sql.DB
	config   ServerConfig
	server   *http.Server
	version  string
	build    string          // Git commit or build hash for strict version matching
	executor CommandExecutor // Executes CLI commands
}

// NewServer creates a new HTTP server instance.
func NewServer(instance *Instance, db *sql.DB, config ServerConfig, version, build string) *Server {
	if config.ListenAddr == "" {
		config.ListenAddr = ":42107"
	}

	s := &Server{
		instance: instance,
		db:       db,
		config:   config,
		version:  version,
		build:    build,
	}

	mux := http.NewServeMux()

	// Health endpoint - unauthenticated
	mux.HandleFunc("/healthz", s.handleHealthz)

	// API endpoints - authenticated with version verification
	mux.HandleFunc("/api/status", s.requireAuth(s.requireVersion(s.handleAPIStatus)))
	mux.HandleFunc("/api/status/", s.requireAuth(s.requireVersion(s.handleAPIStatusDeployment)))
	mux.HandleFunc("/api/sync/", s.requireAuth(s.requireVersion(s.handleAPISync)))
	mux.HandleFunc("/api/deploy/", s.requireAuth(s.requireVersion(s.handleAPIDeploy)))
	mux.HandleFunc("/api/check/", s.requireAuth(s.requireVersion(s.handleAPICheck)))
	mux.HandleFunc("/api/exec", s.requireAuth(s.requireVersion(s.handleAPIExec)))

	s.server = &http.Server{
		Addr:         config.ListenAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s
}

// SetExecutor sets the command executor for the /api/exec endpoint.
func (s *Server) SetExecutor(executor CommandExecutor) {
	s.executor = executor
}

// Start starts the HTTP server in a goroutine.
func (s *Server) Start() error {
	go func() {
		log.Printf("HTTP server listening on %s", s.config.ListenAddr)
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server error: %v", err)
		}
	}()
	return nil
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// requireAuth wraps a handler with admin authentication.
func (s *Server) requireAuth(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			s.jsonError(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}

		if !strings.HasPrefix(auth, "Bearer ") {
			s.jsonError(w, http.StatusUnauthorized, "invalid Authorization header format")
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		if !secureCompare(token, s.config.AdminKey) {
			s.jsonError(w, http.StatusUnauthorized, "invalid admin key")
			return
		}

		handler(w, r)
	}
}

// requireVersion wraps a handler with version verification.
// Stevedore binaries must match exactly - this prevents subtle bugs from version mismatches.
func (s *Server) requireVersion(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientVersion := r.Header.Get(HeaderStevedoreVersion)
		clientBuild := r.Header.Get(HeaderStevedoreBuild)

		// If client doesn't send version headers, reject the request
		if clientVersion == "" || clientBuild == "" {
			s.jsonError(w, http.StatusBadRequest, fmt.Sprintf(
				"missing version headers (expected %s and %s). "+
					"Are you using the correct stevedore binary? Run 'stevedore doctor' to diagnose.",
				HeaderStevedoreVersion, HeaderStevedoreBuild))
			return
		}

		// Strict version matching - binaries must be identical
		if clientVersion != s.version || clientBuild != s.build {
			s.jsonError(w, http.StatusConflict, fmt.Sprintf(
				"version mismatch: client=%s/%s, daemon=%s/%s. "+
					"Stevedore binaries must match exactly. "+
					"Run 'stevedore doctor' to diagnose or reinstall stevedore.",
				clientVersion, clientBuild, s.version, s.build))
			return
		}

		handler(w, r)
	}
}

// handleHealthz handles the health check endpoint.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	response := map[string]interface{}{
		"status":  "ok",
		"version": s.version,
		"build":   s.build,
	}

	s.jsonResponse(w, http.StatusOK, response)
}

// handleAPIStatus handles GET /api/status - list all deployments.
func (s *Server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ctx := r.Context()

	deployments, err := s.instance.ListDeployments()
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, fmt.Sprintf("list deployments: %v", err))
		return
	}

	var results []map[string]interface{}
	for _, d := range deployments {
		status, err := s.instance.GetDeploymentStatus(ctx, d)
		if err != nil {
			results = append(results, map[string]interface{}{
				"deployment": d,
				"error":      err.Error(),
			})
			continue
		}

		syncStatus, _ := s.instance.GetSyncStatus(s.db, d)

		result := map[string]interface{}{
			"deployment":  d,
			"healthy":     status.Healthy,
			"message":     status.Message,
			"containers":  len(status.Containers),
			"projectName": status.ProjectName,
		}

		if syncStatus != nil && syncStatus.LastCommit != "" {
			result["lastCommit"] = syncStatus.LastCommit
			if !syncStatus.LastSyncAt.IsZero() {
				result["lastSyncAt"] = syncStatus.LastSyncAt.Format(time.RFC3339)
			}
			if !syncStatus.LastDeployAt.IsZero() {
				result["lastDeployAt"] = syncStatus.LastDeployAt.Format(time.RFC3339)
			}
			if syncStatus.LastError != "" {
				result["lastError"] = syncStatus.LastError
			}
		}

		results = append(results, result)
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"deployments": results,
	})
}

// handleAPIStatusDeployment handles GET /api/status/{name} - get specific deployment status.
func (s *Server) handleAPIStatusDeployment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	deployment := strings.TrimPrefix(r.URL.Path, "/api/status/")
	if deployment == "" {
		s.jsonError(w, http.StatusBadRequest, "missing deployment name")
		return
	}

	if err := ValidateDeploymentName(deployment); err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()

	status, err := s.instance.GetDeploymentStatus(ctx, deployment)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, fmt.Sprintf("get status: %v", err))
		return
	}

	syncStatus, _ := s.instance.GetSyncStatus(s.db, deployment)

	containers := make([]map[string]interface{}, len(status.Containers))
	for i, c := range status.Containers {
		containers[i] = map[string]interface{}{
			"id":      c.ID,
			"name":    c.Name,
			"service": c.Service,
			"image":   c.Image,
			"state":   string(c.State),
			"health":  string(c.Health),
			"status":  c.Status,
		}
	}

	result := map[string]interface{}{
		"deployment":  deployment,
		"projectName": status.ProjectName,
		"healthy":     status.Healthy,
		"message":     status.Message,
		"containers":  containers,
	}

	if syncStatus != nil {
		result["lastCommit"] = syncStatus.LastCommit
		if !syncStatus.LastSyncAt.IsZero() {
			result["lastSyncAt"] = syncStatus.LastSyncAt.Format(time.RFC3339)
		}
		if !syncStatus.LastDeployAt.IsZero() {
			result["lastDeployAt"] = syncStatus.LastDeployAt.Format(time.RFC3339)
		}
		if syncStatus.LastError != "" {
			result["lastError"] = syncStatus.LastError
			if !syncStatus.LastErrorAt.IsZero() {
				result["lastErrorAt"] = syncStatus.LastErrorAt.Format(time.RFC3339)
			}
		}
	}

	s.jsonResponse(w, http.StatusOK, result)
}

// handleAPISync handles POST /api/sync/{name} - trigger sync for a deployment.
func (s *Server) handleAPISync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	deployment := strings.TrimPrefix(r.URL.Path, "/api/sync/")
	if deployment == "" {
		s.jsonError(w, http.StatusBadRequest, "missing deployment name")
		return
	}

	if err := ValidateDeploymentName(deployment); err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()

	log.Printf("API: triggering sync for %s", deployment)

	result, err := s.instance.GitCloneLocal(ctx, deployment)
	if err != nil {
		_ = s.instance.UpdateSyncError(s.db, deployment, err)
		s.jsonError(w, http.StatusInternalServerError, fmt.Sprintf("sync failed: %v", err))
		return
	}

	if err := s.instance.UpdateSyncStatus(s.db, deployment, result.Commit); err != nil {
		log.Printf("warning: failed to update sync status: %v", err)
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"deployment": deployment,
		"commit":     result.Commit,
		"branch":     result.Branch,
		"synced":     true,
	})
}

// handleAPIDeploy handles POST /api/deploy/{name} - trigger deploy for a deployment.
func (s *Server) handleAPIDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	deployment := strings.TrimPrefix(r.URL.Path, "/api/deploy/")
	if deployment == "" {
		s.jsonError(w, http.StatusBadRequest, "missing deployment name")
		return
	}

	if err := ValidateDeploymentName(deployment); err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()

	log.Printf("API: triggering deploy for %s", deployment)

	result, err := s.instance.Deploy(ctx, deployment, ComposeConfig{})
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, fmt.Sprintf("deploy failed: %v", err))
		return
	}

	if err := s.instance.UpdateDeployStatus(s.db, deployment); err != nil {
		log.Printf("warning: failed to update deploy status: %v", err)
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"deployment":  deployment,
		"projectName": result.ProjectName,
		"composeFile": result.ComposeFile,
		"services":    result.Services,
		"deployed":    true,
	})
}

// handleAPICheck handles POST /api/check/{name} - check for updates without modifying files.
// This performs a git fetch only and compares commits, safe to call while deployment is running.
func (s *Server) handleAPICheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	deployment := strings.TrimPrefix(r.URL.Path, "/api/check/")
	if deployment == "" {
		s.jsonError(w, http.StatusBadRequest, "missing deployment name")
		return
	}

	if err := ValidateDeploymentName(deployment); err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()

	log.Printf("API: checking for updates for %s", deployment)

	result, err := s.instance.GitCheckRemote(ctx, deployment)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, fmt.Sprintf("check failed: %v", err))
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"deployment":    deployment,
		"currentCommit": result.CurrentCommit,
		"remoteCommit":  result.RemoteCommit,
		"hasChanges":    result.HasChanges,
		"branch":        result.Branch,
	})
}

// ExecRequest represents a request to execute a command.
type ExecRequest struct {
	Args []string `json:"args"`
}

// ExecResponse represents the response from command execution.
type ExecResponse struct {
	Output   string `json:"output"`
	ExitCode int    `json:"exitCode"`
	Error    string `json:"error,omitempty"`
}

// handleAPIExec handles POST /api/exec - execute a CLI command inside the daemon.
// This allows the CLI to delegate commands to the daemon process.
func (s *Server) handleAPIExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.executor == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "command executor not configured")
		return
	}

	var req ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}

	log.Printf("API: executing command: %v", req.Args)

	output, exitCode, err := s.executor(req.Args)

	resp := ExecResponse{
		Output:   output,
		ExitCode: exitCode,
	}
	if err != nil {
		resp.Error = err.Error()
	}

	s.jsonResponse(w, http.StatusOK, resp)
}

// jsonResponse writes a JSON response with the given status code.
func (s *Server) jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("error encoding JSON response: %v", err)
	}
}

// jsonError writes a JSON error response.
func (s *Server) jsonError(w http.ResponseWriter, status int, message string) {
	s.jsonResponse(w, status, map[string]string{"error": message})
}
