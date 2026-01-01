package stevedore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestQueryServer_Healthz(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	qs := NewQueryServer(instance, "")

	// Create HTTP handler
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", qs.handleHealthz)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("status = %q, want %q", response["status"], "ok")
	}
}

func TestQueryServer_RequireAuth(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	// Create deployment for token
	deploymentDir := instance.DeploymentDir("testapp")
	if err := os.MkdirAll(deploymentDir, 0o755); err != nil {
		t.Fatalf("failed to create deployment dir: %v", err)
	}

	// Generate token
	token, err := instance.EnsureQueryToken("testapp")
	if err != nil {
		t.Fatalf("EnsureQueryToken: %v", err)
	}

	qs := NewQueryServer(instance, "")

	// Create handler with auth middleware
	mux := http.NewServeMux()
	mux.HandleFunc("/deployments", qs.handleDeployments)
	handler := qs.requireAuth(mux)

	tests := []struct {
		name       string
		authHeader string
		wantCode   int
	}{
		{"no auth", "", http.StatusUnauthorized},
		{"invalid format", "invalid", http.StatusUnauthorized},
		{"wrong token", "Bearer wrong-token", http.StatusUnauthorized},
		{"valid token", "Bearer " + token, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/deployments", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("status code = %d, want %d", w.Code, tt.wantCode)
			}
		})
	}
}

func TestQueryServer_HandleDeployments(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	// Create some deployments
	for _, name := range []string{"app1", "app2", "app3"} {
		deploymentDir := instance.DeploymentDir(name)
		if err := os.MkdirAll(deploymentDir, 0o755); err != nil {
			t.Fatalf("failed to create deployment dir: %v", err)
		}
	}

	qs := NewQueryServer(instance, "")

	req := httptest.NewRequest(http.MethodGet, "/deployments", nil)
	w := httptest.NewRecorder()

	qs.handleDeployments(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var deployments []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &deployments); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(deployments) != 3 {
		t.Errorf("got %d deployments, want 3", len(deployments))
	}
}

func TestQueryServer_NotifyChange(t *testing.T) {
	instance := NewInstance(t.TempDir())
	qs := NewQueryServer(instance, "")

	// Record initial change time
	initialTime := qs.lastChangeAt

	// Wait a tiny bit to ensure time difference
	time.Sleep(10 * time.Millisecond)

	// Notify change
	qs.NotifyChange()

	// Verify time was updated
	if !qs.lastChangeAt.After(initialTime) {
		t.Error("NotifyChange should update lastChangeAt")
	}
}

func TestQueryServer_NotifyChangeWakesWaiters(t *testing.T) {
	instance := NewInstance(t.TempDir())
	qs := NewQueryServer(instance, "")

	// Add a waiter
	waiter := make(chan struct{}, 1)
	qs.mu.Lock()
	qs.changeWaiters = append(qs.changeWaiters, waiter)
	qs.mu.Unlock()

	// Notify change
	qs.NotifyChange()

	// Waiter should receive notification
	select {
	case <-waiter:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Error("waiter should have received notification")
	}

	// Waiters list should be cleared
	qs.mu.RLock()
	if len(qs.changeWaiters) != 0 {
		t.Errorf("changeWaiters should be empty, got %d", len(qs.changeWaiters))
	}
	qs.mu.RUnlock()
}

func TestQueryServer_SocketPath(t *testing.T) {
	instance := NewInstance(t.TempDir())

	// Default path
	qs1 := NewQueryServer(instance, "")
	if qs1.SocketPath() != DefaultQuerySocketPath {
		t.Errorf("SocketPath() = %q, want %q", qs1.SocketPath(), DefaultQuerySocketPath)
	}

	// Custom path
	customPath := "/tmp/test-query.sock"
	qs2 := NewQueryServer(instance, customPath)
	if qs2.SocketPath() != customPath {
		t.Errorf("SocketPath() = %q, want %q", qs2.SocketPath(), customPath)
	}
}
