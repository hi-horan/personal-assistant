package modelx

import (
	"testing"

	"personal-assistant/internal/config"
)

func TestResolveProviderConfigGLMLegacy(t *testing.T) {
	cfg := config.Defaults()
	cfg.ModelProvider = "glm"
	cfg.GLMModel = "glm-test"
	cfg.GLMAPIKey = "secret"
	cfg.GLMBaseURL = "https://example.test/v1"
	cfg.GLMThinkingType = "enabled"

	got, err := ResolveProviderConfig(cfg, cfg.ModelProvider)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != ProviderTypeOpenAICompat {
		t.Fatalf("type = %q", got.Type)
	}
	if got.Model != "glm-test" || got.APIKey != "secret" || got.BaseURL != "https://example.test/v1" {
		t.Fatalf("unexpected provider config: %+v", got)
	}
	if got.ThinkingType != "enabled" {
		t.Fatalf("thinking type = %q", got.ThinkingType)
	}
}

func TestResolveProviderConfigMapOverride(t *testing.T) {
	cfg := config.Defaults()
	cfg.ModelProvider = "openai"
	cfg.Providers = map[string]config.ProviderConfig{
		"openai": {
			Model:           "gpt-test",
			APIKey:          "key",
			BaseURL:         "https://proxy.test/v1",
			ReasoningEffort: "medium",
		},
	}

	got, err := ResolveProviderConfig(cfg, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "gpt-test" || got.BaseURL != "https://proxy.test/v1" {
		t.Fatalf("unexpected provider config: %+v", got)
	}
	if got.ReasoningEffort != "medium" {
		t.Fatalf("reasoning effort = %q", got.ReasoningEffort)
	}
}
