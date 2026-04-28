package modelx

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"personal-assistant/internal/config"

	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

func New(ctx context.Context, cfg *config.Config, logger *slog.Logger) (model.LLM, ProviderRuntimeConfig, error) {
	providerName := strings.ToLower(strings.TrimSpace(cfg.ModelProvider))
	if providerName == "" {
		providerName = "echo"
	}
	provider, err := ResolveProviderConfig(cfg, providerName)
	if err != nil {
		return nil, ProviderRuntimeConfig{}, err
	}
	if provider.Type == "" {
		provider.Type = providerName
	}

	switch provider.Type {
	case ProviderTypeEcho:
		llm := NewEchoModel(provider.Model)
		return llm, provider, nil
	case ProviderTypeGemini:
		llm, err := gemini.NewModel(ctx, provider.Model, &genai.ClientConfig{
			APIKey:  provider.APIKey,
			Backend: genai.BackendGeminiAPI,
		})
		if err != nil {
			return nil, ProviderRuntimeConfig{}, fmt.Errorf("create gemini model: %w", err)
		}
		return llm, provider, nil
	case ProviderTypeOpenAICompat:
		llm := NewOpenAICompatModel(OpenAICompatConfig{
			ProviderName:    provider.Name,
			Model:           provider.Model,
			APIKey:          provider.APIKey,
			BaseURL:         provider.BaseURL,
			APIPath:         provider.APIPath,
			Headers:         provider.Headers,
			Timeout:         time.Duration(provider.TimeoutSeconds) * time.Second,
			ReasoningEffort: provider.ReasoningEffort,
			ThinkingType:    provider.ThinkingType,
			ServiceTier:     provider.ServiceTier,
			Logger:          logger,
		})
		return llm, provider, nil
	case ProviderTypeAnthropic:
		llm := NewAnthropicModel(AnthropicConfig{
			ProviderName:    provider.Name,
			Model:           provider.Model,
			APIKey:          provider.APIKey,
			BaseURL:         provider.BaseURL,
			Headers:         provider.Headers,
			Timeout:         time.Duration(provider.TimeoutSeconds) * time.Second,
			ReasoningEffort: provider.ReasoningEffort,
			Logger:          logger,
		})
		return llm, provider, nil
	default:
		return nil, ProviderRuntimeConfig{}, fmt.Errorf("unsupported provider type %q for provider %q", provider.Type, provider.Name)
	}
}
