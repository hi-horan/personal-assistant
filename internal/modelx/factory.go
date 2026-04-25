package modelx

import (
	"context"
	"fmt"

	"personal-assistant/internal/config"

	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

func New(ctx context.Context, cfg config.Config) (model.LLM, error) {
	switch cfg.ModelProvider {
	case "echo":
		return NewEchoModel(), nil
	case "gemini":
		return gemini.NewModel(ctx, cfg.GeminiModel, &genai.ClientConfig{
			APIKey:  cfg.GeminiAPIKey,
			Backend: genai.BackendGeminiAPI,
		})
	default:
		return nil, fmt.Errorf("unsupported model provider %q", cfg.ModelProvider)
	}
}
