package stevedore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// ServerConfig holds configuration for the HTTP server.
type ServerConfig struct {
	AdminKey   string
	ListenAddr string
}

// Server provides the HTTP API for Stevedore.
type Server struct {
	instance *Instance
	db       *sql.DB
	config   ServerConfig
	server   *http.Server
	version  string
}

// NewServer creates a new HTTP server instance.
func NewServer(instance *Instance, db *sql.DB, config ServerConfig, version string) *Server {
	if config.ListenAddr == "" {
		config.ListenAddr = ":42107"
	}

	s := &Server{
		instance: instance,
		db:       db,
		config:   config,
		version:  version,
	}

	mux := http.NewServeMux()

	// Health endpoint - unauthenticated
	mux.HandleFunc("/healthz", s.handleHealthz)

	// API endpoints - authenticated
	mux.HandleFunc("/api/status", s.requireAuth(s.handleAPIStatus))
	mux.HandleFunc("/api/status/", s.requireAuth(s.handleAPIStatusDeployment))
	mux.HandleFunc("/api/sync/", s.requireAuth(s.handleAPISync))
	mux.HandleFunc("/api/deploy/", s.requireAuth(s.handleAPIDeploy))

	s.server = &http.Server{
		Addr:         config.ListenAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return s
}

// Start starts the HTTP server in a goroutine.
func (s *Server) Start() error {
	go func() {
		log.Printf("HTTP server listening on %s", s.config.ListenAddr)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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

// handleHealthz handles the health check endpoint.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	response := map[string]interface{}{
		"status":  "ok",
		"version": s.version,
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
