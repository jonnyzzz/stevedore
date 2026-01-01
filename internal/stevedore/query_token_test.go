package stevedore

import (
	"os"
	"testing"
)

func TestGenerateQueryToken(t *testing.T) {
	token1, err := GenerateQueryToken()
	if err != nil {
		t.Fatalf("GenerateQueryToken: %v", err)
	}

	// Token should be hex-encoded, so 2x the byte length
	expectedLen := QueryTokenLength * 2
	if len(token1) != expectedLen {
		t.Errorf("token length = %d, want %d", len(token1), expectedLen)
	}

	// Generate another token - should be different
	token2, err := GenerateQueryToken()
	if err != nil {
		t.Fatalf("GenerateQueryToken: %v", err)
	}

	if token1 == token2 {
		t.Error("two generated tokens should be different")
	}
}

func TestEnsureQueryToken(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	// Create deployment directory
	deploymentDir := instance.DeploymentDir("testapp")
	if err := os.MkdirAll(deploymentDir, 0o755); err != nil {
		t.Fatalf("failed to create deployment dir: %v", err)
	}

	// First call should generate a new token
	token1, err := instance.EnsureQueryToken("testapp")
	if err != nil {
		t.Fatalf("EnsureQueryToken: %v", err)
	}

	if token1 == "" {
		t.Error("token should not be empty")
	}

	// Second call should return the same token
	token2, err := instance.EnsureQueryToken("testapp")
	if err != nil {
		t.Fatalf("EnsureQueryToken second call: %v", err)
	}

	if token1 != token2 {
		t.Errorf("EnsureQueryToken returned different tokens: %q vs %q", token1, token2)
	}
}

func TestGetQueryToken(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	deploymentDir := instance.DeploymentDir("testapp")
	if err := os.MkdirAll(deploymentDir, 0o755); err != nil {
		t.Fatalf("failed to create deployment dir: %v", err)
	}

	// Get token for deployment without one should fail
	_, err := instance.GetQueryToken("testapp")
	if err == nil {
		t.Error("GetQueryToken expected error for deployment without token")
	}

	// Create a token
	createdToken, err := instance.EnsureQueryToken("testapp")
	if err != nil {
		t.Fatalf("EnsureQueryToken: %v", err)
	}

	// Now GetQueryToken should return it
	retrievedToken, err := instance.GetQueryToken("testapp")
	if err != nil {
		t.Fatalf("GetQueryToken: %v", err)
	}

	if retrievedToken != createdToken {
		t.Errorf("GetQueryToken = %q, want %q", retrievedToken, createdToken)
	}
}

func TestValidateQueryToken(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	deploymentDir := instance.DeploymentDir("testapp")
	if err := os.MkdirAll(deploymentDir, 0o755); err != nil {
		t.Fatalf("failed to create deployment dir: %v", err)
	}

	// Create a token
	token, err := instance.EnsureQueryToken("testapp")
	if err != nil {
		t.Fatalf("EnsureQueryToken: %v", err)
	}

	// Validate the token
	deployment, err := instance.ValidateQueryToken(token)
	if err != nil {
		t.Fatalf("ValidateQueryToken: %v", err)
	}

	if deployment != "testapp" {
		t.Errorf("ValidateQueryToken deployment = %q, want %q", deployment, "testapp")
	}

	// Invalid token should fail
	_, err = instance.ValidateQueryToken("invalid-token")
	if err == nil {
		t.Error("ValidateQueryToken expected error for invalid token")
	}

	// Empty token should fail
	_, err = instance.ValidateQueryToken("")
	if err == nil {
		t.Error("ValidateQueryToken expected error for empty token")
	}
}

func TestRegenerateQueryToken(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	deploymentDir := instance.DeploymentDir("testapp")
	if err := os.MkdirAll(deploymentDir, 0o755); err != nil {
		t.Fatalf("failed to create deployment dir: %v", err)
	}

	// Create initial token
	token1, err := instance.EnsureQueryToken("testapp")
	if err != nil {
		t.Fatalf("EnsureQueryToken: %v", err)
	}

	// Regenerate token
	token2, err := instance.RegenerateQueryToken("testapp")
	if err != nil {
		t.Fatalf("RegenerateQueryToken: %v", err)
	}

	if token1 == token2 {
		t.Error("regenerated token should be different from original")
	}

	// Old token should be invalid
	_, err = instance.ValidateQueryToken(token1)
	if err == nil {
		t.Error("old token should be invalid after regeneration")
	}

	// New token should be valid
	deployment, err := instance.ValidateQueryToken(token2)
	if err != nil {
		t.Fatalf("ValidateQueryToken with new token: %v", err)
	}
	if deployment != "testapp" {
		t.Errorf("deployment = %q, want %q", deployment, "testapp")
	}
}

func TestListQueryTokens(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	if err := instance.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}

	// Create deployment directories
	for _, name := range []string{"app1", "app2", "app3"} {
		deploymentDir := instance.DeploymentDir(name)
		if err := os.MkdirAll(deploymentDir, 0o755); err != nil {
			t.Fatalf("failed to create deployment dir: %v", err)
		}
	}

	// Create tokens for some deployments
	token1, _ := instance.EnsureQueryToken("app1")
	token2, _ := instance.EnsureQueryToken("app2")

	// List tokens
	tokens, err := instance.ListQueryTokens()
	if err != nil {
		t.Fatalf("ListQueryTokens: %v", err)
	}

	if len(tokens) != 2 {
		t.Errorf("ListQueryTokens returned %d tokens, want 2", len(tokens))
	}

	if tokens["app1"] != token1 {
		t.Errorf("tokens[app1] = %q, want %q", tokens["app1"], token1)
	}
	if tokens["app2"] != token2 {
		t.Errorf("tokens[app2] = %q, want %q", tokens["app2"], token2)
	}
}

func TestQueryToken_InvalidDeploymentName(t *testing.T) {
	instance := NewInstance(t.TempDir())
	t.Setenv("STEVEDORE_DB_KEY", "test-key")

	_, err := instance.EnsureQueryToken("-invalid")
	if err == nil {
		t.Error("EnsureQueryToken expected error for invalid deployment name")
	}
}
