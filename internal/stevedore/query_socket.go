package stevedore

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultQuerySocketPath is the default path for the query socket.
	DefaultQuerySocketPath = "/var/run/stevedore/query.sock"

	// QuerySocketTimeout is the timeout for query socket operations.
	QuerySocketTimeout = 30 * time.Second

	// LongPollTimeout is the timeout for long-polling requests.
	LongPollTimeout = 60 * time.Second
)

// QueryServer handles read-only API queries over a Unix domain socket.
type QueryServer struct {
	instance   *Instance
	socketPath string
	listener   net.Listener

	// For long-polling: track deployment changes
	mu            sync.RWMutex
	lastChangeAt  time.Time
	changeWaiters []chan struct{}
}

// NewQueryServer creates a new query server.
func NewQueryServer(instance *Instance, socketPath string) *QueryServer {
	if socketPath == "" {
		socketPath = DefaultQuerySocketPath
	}
	return &QueryServer{
		instance:     instance,
		socketPath:   socketPath,
		lastChangeAt: time.Now(),
	}
}

// Start starts the query server.
func (qs *QueryServer) Start(ctx context.Context) error {
	// Ensure socket directory exists
	socketDir := filepath.Dir(qs.socketPath)
	if err := os.MkdirAll(socketDir, 0o755); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Remove existing socket file
	if err := os.Remove(qs.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket: %w", err)
	}

	// Create Unix domain socket listener
	listener, err := net.Listen("unix", qs.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}
	qs.listener = listener

	// Set socket permissions (readable by all, for containers)
	if err := os.Chmod(qs.socketPath, 0o666); err != nil {
		_ = listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	log.Printf("Query socket listening on %s", qs.socketPath)

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/services", qs.handleServices)
	mux.HandleFunc("/deployments", qs.handleDeployments)
	mux.HandleFunc("/status/", qs.handleStatus)
	mux.HandleFunc("/poll", qs.handlePoll)
	mux.HandleFunc("/healthz", qs.handleHealthz)

	server := &http.Server{
		Handler:      qs.requireAuth(mux),
		ReadTimeout:  QuerySocketTimeout,
		WriteTimeout: LongPollTimeout + 10*time.Second, // Allow for long-poll
	}

	// Start server in goroutine
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("Query server error: %v", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()
	return server.Shutdown(context.Background())
}

// Stop stops the query server.
func (qs *QueryServer) Stop() error {
	if qs.listener != nil {
		return qs.listener.Close()
	}
	return nil
}

// NotifyChange notifies all long-polling clients of a deployment change.
func (qs *QueryServer) NotifyChange() {
	qs.mu.Lock()
	defer qs.mu.Unlock()

	qs.lastChangeAt = time.Now()

	// Notify all waiters
	for _, ch := range qs.changeWaiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	qs.changeWaiters = nil
}

// requireAuth wraps handlers with token authentication.
func (qs *QueryServer) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Healthz doesn't require auth
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		// Extract token from Authorization header
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "Missing or invalid Authorization header", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")

		// Validate token
		deployment, err := qs.instance.ValidateQueryToken(token)
		if err != nil {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		// Store deployment in context for handlers
		ctx := context.WithValue(r.Context(), queryDeploymentKey, deployment)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type contextKey string

const queryDeploymentKey contextKey = "query_deployment"

func (qs *QueryServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (qs *QueryServer) handleServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	ingressOnly := r.URL.Query().Get("ingress") == "true"

	var services []Service
	var err error

	if ingressOnly {
		services, err = qs.instance.ListIngressServices(ctx)
	} else {
		services, err = qs.instance.ListServices(ctx)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(services)
}

func (qs *QueryServer) handleDeployments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	deployments, err := qs.instance.ListDeployments()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type deploymentInfo struct {
		Name string `json:"name"`
	}

	result := make([]deploymentInfo, len(deployments))
	for i, d := range deployments {
		result[i] = deploymentInfo{Name: d}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (qs *QueryServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract deployment name from path: /status/{name}
	name := strings.TrimPrefix(r.URL.Path, "/status/")
	if name == "" {
		http.Error(w, "Missing deployment name", http.StatusBadRequest)
		return
	}

	status, err := qs.instance.GetDeploymentStatus(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (qs *QueryServer) handlePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get since parameter (Unix timestamp)
	sinceStr := r.URL.Query().Get("since")
	var since time.Time
	if sinceStr != "" {
		var sinceUnix int64
		if _, err := fmt.Sscanf(sinceStr, "%d", &sinceUnix); err == nil {
			since = time.Unix(sinceUnix, 0)
		}
	}

	// Check if there's already a change since the given time
	qs.mu.RLock()
	if !since.IsZero() && qs.lastChangeAt.After(since) {
		qs.mu.RUnlock()
		qs.sendPollResponse(w)
		return
	}
	qs.mu.RUnlock()

	// Set up long-polling
	waiter := make(chan struct{}, 1)
	qs.mu.Lock()
	qs.changeWaiters = append(qs.changeWaiters, waiter)
	qs.mu.Unlock()

	// Wait for change or timeout
	ctx := r.Context()
	timeout := time.NewTimer(LongPollTimeout)
	defer timeout.Stop()

	select {
	case <-waiter:
		qs.sendPollResponse(w)
	case <-timeout.C:
		// No change within timeout
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"changed":false}`))
	case <-ctx.Done():
		// Client disconnected
		return
	}
}

func (qs *QueryServer) sendPollResponse(w http.ResponseWriter) {
	qs.mu.RLock()
	changeAt := qs.lastChangeAt.Unix()
	qs.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"changed":true,"timestamp":%d}`, changeAt)
}

// SocketPath returns the socket path.
func (qs *QueryServer) SocketPath() string {
	return qs.socketPath
}
