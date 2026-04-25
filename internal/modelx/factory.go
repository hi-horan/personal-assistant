package modelx

import (
	"context"
	"fmt"
	"log/slog"

	"personal-assistant/internal/config"

	"google.golang.org/adk/model"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (model.LLM, error) {
	switch cfg.ModelProvider {
	case "echo":
		return NewEchoModel(), nil
	case "gemini":
		return gemini.NewModel(ctx, cfg.GeminiModel, &genai.ClientConfig{
			APIKey:  cfg.GeminiAPIKey,
			Backend: genai.BackendGeminiAPI,
		})
	case "glm":
		return NewGLMModel(cfg, logger), nil
	default:
		return nil, fmt.Errorf("unsupported model provider %q", cfg.ModelProvider)
	}
}
