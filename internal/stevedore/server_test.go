package stevedore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz_ReturnsOK(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	instance := NewInstance(tmpDir)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	server := NewServer(instance, db, ServerConfig{
		AdminKey:   "test-admin-key",
		ListenAddr: ":0",
	}, "1.0.0-test", "test-build")

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	server.handleHealthz(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", response["status"])
	}

	if response["version"] != "1.0.0-test" {
		t.Errorf("expected version '1.0.0-test', got %v", response["version"])
	}
}

func TestHealthz_MethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	instance := NewInstance(tmpDir)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	server := NewServer(instance, db, ServerConfig{
		AdminKey: "test-admin-key",
	}, "1.0.0", "test-build")

	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	w := httptest.NewRecorder()

	server.handleHealthz(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}

func TestRequireAuth_ValidKey(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	instance := NewInstance(tmpDir)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	server := NewServer(instance, db, ServerConfig{
		AdminKey: "secret-admin-key",
	}, "1.0.0", "test-build")

	handlerCalled := false
	handler := server.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer secret-admin-key")
	w := httptest.NewRecorder()

	handler(w, req)

	if !handlerCalled {
		t.Error("expected handler to be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestRequireAuth_InvalidKey(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	instance := NewInstance(tmpDir)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	server := NewServer(instance, db, ServerConfig{
		AdminKey: "secret-admin-key",
	}, "1.0.0", "test-build")

	handlerCalled := false
	handler := server.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	w := httptest.NewRecorder()

	handler(w, req)

	if handlerCalled {
		t.Error("expected handler NOT to be called")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestRequireAuth_MissingHeader(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	instance := NewInstance(tmpDir)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	server := NewServer(instance, db, ServerConfig{
		AdminKey: "secret-admin-key",
	}, "1.0.0", "test-build")

	handler := server.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	// No Authorization header
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestRequireAuth_WrongFormat(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	instance := NewInstance(tmpDir)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	server := NewServer(instance, db, ServerConfig{
		AdminKey: "secret-admin-key",
	}, "1.0.0", "test-build")

	handler := server.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Basic secret-admin-key") // Wrong prefix
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestAPIStatus_ListsDeployments(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	instance := NewInstance(tmpDir)
	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout failed: %v", err)
	}

	db, err := instance.OpenDB()
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	server := NewServer(instance, db, ServerConfig{
		AdminKey: "test-admin-key",
	}, "1.0.0", "test-build")

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	server.handleAPIStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if _, ok := response["deployments"]; !ok {
		t.Error("expected 'deployments' field in response")
	}
}

func TestSecureCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"hello", "hello", true},
		{"hello", "world", false},
		{"hello", "hell", false},
		{"", "", true},
		{"a", "", false},
		{"", "a", false},
		{"secret-key-123", "secret-key-123", true},
		{"secret-key-123", "secret-key-124", false},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			got := secureCompare(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("secureCompare(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
