package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFileGLMProvider(t *testing.T) {
	cfg := loadConfigForTest(t, `
model_provider: glm
database_url: "postgres://user:pass@localhost:5432/db?sslmode=disable"
glm_api_key: "test-key"
glm_thinking_type: enabled
`)

	if got, want := cfg.ModelProvider, "glm"; got != want {
		t.Fatalf("ModelProvider = %q, want %q", got, want)
	}
	if got, want := cfg.GLMModel, "glm-4.6v"; got != want {
		t.Fatalf("GLMModel = %q, want %q", got, want)
	}
	if got, want := cfg.GLMBaseURL, "https://open.bigmodel.cn/api/paas/v4"; got != want {
		t.Fatalf("GLMBaseURL = %q, want %q", got, want)
	}
	if got, want := cfg.GLMThinkingType, "enabled"; got != want {
		t.Fatalf("GLMThinkingType = %q, want %q", got, want)
	}
}

func TestLoadFileGLMProviderRequiresAPIKey(t *testing.T) {
	path := writeConfigForTest(t, `
model_provider: glm
database_url: "postgres://user:pass@localhost:5432/db?sslmode=disable"
`)

	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "glm_api_key is required") {
		t.Fatalf("LoadFile() error = %v, want glm_api_key error", err)
	}
}

func TestLoadFileGLMThinkingTypeValidation(t *testing.T) {
	path := writeConfigForTest(t, `
model_provider: glm
database_url: "postgres://user:pass@localhost:5432/db?sslmode=disable"
glm_api_key: "test-key"
glm_thinking_type: maybe
`)

	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "glm_thinking_type") {
		t.Fatalf("LoadFile() error = %v, want glm_thinking_type error", err)
	}
}

func loadConfigForTest(t *testing.T, content string) Config {
	t.Helper()
	cfg, err := LoadFile(writeConfigForTest(t, content))
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	return cfg
}

func writeConfigForTest(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
