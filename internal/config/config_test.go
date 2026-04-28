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
	if !strings.Contains(cfg.Instruction, "个人助手") {
		t.Fatalf("Instruction = %q, want Chinese default instruction", cfg.Instruction)
	}
}

func TestLoadFileInstructionOverride(t *testing.T) {
	cfg := loadConfigForTest(t, `
database_url: "postgres://user:pass@localhost:5432/db?sslmode=disable"
instruction: |
  你是我的个人助手。
  {rag_context?}
`)

	if got, want := cfg.Instruction, "你是我的个人助手。\n{rag_context?}"; got != want {
		t.Fatalf("Instruction = %q, want %q", got, want)
	}
}

func TestLoadFileRejectsEmptyInstruction(t *testing.T) {
	path := writeConfigForTest(t, `
database_url: "postgres://user:pass@localhost:5432/db?sslmode=disable"
instruction: "   "
`)

	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "instruction is required") {
		t.Fatalf("LoadFile() error = %v, want instruction error", err)
	}
}

func TestLoadFileBigModelEmbeddingProvider(t *testing.T) {
	cfg := loadConfigForTest(t, `
database_url: "postgres://user:pass@localhost:5432/db?sslmode=disable"
embedding_provider: bigmodel
embedding_model: embedding-3
bigmodel_api_key: "test-key"
bigmodel_base_url: "https://open.bigmodel.cn/api/paas/v4/"
`)

	if got, want := cfg.EmbeddingProvider, "bigmodel"; got != want {
		t.Fatalf("EmbeddingProvider = %q, want %q", got, want)
	}
	if got, want := cfg.EmbeddingModel, "embedding-3"; got != want {
		t.Fatalf("EmbeddingModel = %q, want %q", got, want)
	}
	if got, want := cfg.BigModelBaseURL, "https://open.bigmodel.cn/api/paas/v4"; got != want {
		t.Fatalf("BigModelBaseURL = %q, want %q", got, want)
	}
	if got, want := cfg.EmbeddingDimension, 1024; got != want {
		t.Fatalf("EmbeddingDimension = %d, want %d", got, want)
	}
}

func TestLoadFileBigModelEmbeddingProviderRequiresAPIKey(t *testing.T) {
	path := writeConfigForTest(t, `
database_url: "postgres://user:pass@localhost:5432/db?sslmode=disable"
embedding_provider: bigmodel
embedding_model: embedding-3
`)

	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "bigmodel_api_key is required") {
		t.Fatalf("LoadFile() error = %v, want bigmodel_api_key error", err)
	}
}

func TestLoadFileRejectsLongAppName(t *testing.T) {
	path := writeConfigForTest(t, `
app_name: "`+strings.Repeat("a", 257)+`"
database_url: "postgres://user:pass@localhost:5432/db?sslmode=disable"
`)

	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "app_name must be at most 256 characters") {
		t.Fatalf("LoadFile() error = %v, want app_name length error", err)
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

func TestLoadFileRejectsMultipleYAMLDocuments(t *testing.T) {
	path := writeConfigForTest(t, `
database_url: "postgres://user:pass@localhost:5432/db?sslmode=disable"
---
database_url: "postgres://other:pass@localhost:5432/db?sslmode=disable"
`)

	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("LoadFile() error = %v, want multiple YAML documents error", err)
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
