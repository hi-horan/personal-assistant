package modelx

import (
	"fmt"
	"os"
	"strings"

	"personal-assistant/internal/config"
)

const (
	ProviderTypeEcho         = "echo"
	ProviderTypeGemini       = "gemini"
	ProviderTypeOpenAICompat = "openai_compat"
	ProviderTypeAnthropic    = "anthropic"
)

type ProviderRuntimeConfig struct {
	Name            string
	Type            string
	Model           string
	APIKey          string
	BaseURL         string
	APIPath         string
	Headers         map[string]string
	TimeoutSeconds  int
	ReasoningEffort string
	ThinkingType    string
	ServiceTier     string
}

func ResolveProviderConfig(cfg *config.Config, name string) (ProviderRuntimeConfig, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "echo"
	}

	spec, ok := defaultProviderSpecs()[name]
	if !ok {
		return ProviderRuntimeConfig{}, fmt.Errorf("unknown model provider %q", name)
	}
	runtime := spec
	runtime.Name = name

	if user, ok := cfg.Providers[name]; ok {
		mergeProviderConfig(&runtime, user)
	}
	mergeLegacyConfig(&runtime, cfg)

	if runtime.Model == "" {
		return ProviderRuntimeConfig{}, fmt.Errorf("provider %q model is required", name)
	}
	if runtime.Type == ProviderTypeOpenAICompat && runtime.BaseURL == "" {
		return ProviderRuntimeConfig{}, fmt.Errorf("provider %q base_url is required", name)
	}
	if runtime.TimeoutSeconds <= 0 {
		runtime.TimeoutSeconds = 90
	}
	return runtime, nil
}

func mergeProviderConfig(dst *ProviderRuntimeConfig, src config.ProviderConfig) {
	if src.Type != "" {
		dst.Type = src.Type
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.APIKey != "" {
		dst.APIKey = src.APIKey
	}
	if src.BaseURL != "" {
		dst.BaseURL = src.BaseURL
	}
	if src.APIPath != "" {
		dst.APIPath = src.APIPath
	}
	if src.TimeoutSeconds != 0 {
		dst.TimeoutSeconds = src.TimeoutSeconds
	}
	if src.ReasoningEffort != "" {
		dst.ReasoningEffort = src.ReasoningEffort
	}
	if src.ThinkingType != "" {
		dst.ThinkingType = src.ThinkingType
	}
	if src.ServiceTier != "" {
		dst.ServiceTier = src.ServiceTier
	}
	if len(src.Headers) > 0 {
		dst.Headers = src.Headers
	}
	if src.Enabled != nil && !*src.Enabled {
		dst.Type = "disabled"
	}
}

func mergeLegacyConfig(dst *ProviderRuntimeConfig, cfg *config.Config) {
	switch dst.Name {
	case "gemini":
		if cfg.GeminiModel != "" {
			dst.Model = cfg.GeminiModel
		}
		if cfg.GeminiAPIKey != "" {
			dst.APIKey = cfg.GeminiAPIKey
		}
	case "glm":
		if cfg.GLMModel != "" {
			dst.Model = cfg.GLMModel
		}
		if cfg.GLMAPIKey != "" {
			dst.APIKey = cfg.GLMAPIKey
		}
		if cfg.GLMBaseURL != "" {
			dst.BaseURL = cfg.GLMBaseURL
		}
		if cfg.GLMThinkingType != "" {
			dst.ThinkingType = cfg.GLMThinkingType
		}
	case "bigmodel":
		if cfg.BigModelAPIKey != "" {
			dst.APIKey = cfg.BigModelAPIKey
		}
		if cfg.BigModelBaseURL != "" {
			dst.BaseURL = cfg.BigModelBaseURL
		}
	}
}

func defaultProviderSpecs() map[string]ProviderRuntimeConfig {
	return map[string]ProviderRuntimeConfig{
		"echo":        {Type: ProviderTypeEcho, Model: "echo"},
		"gemini":      {Type: ProviderTypeGemini, Model: "gemini-2.5-flash", APIKey: os.Getenv("GEMINI_API_KEY")},
		"anthropic":   {Type: ProviderTypeAnthropic, Model: "claude-sonnet-4-5", BaseURL: "https://api.anthropic.com", APIKey: os.Getenv("ANTHROPIC_API_KEY")},
		"openai":      {Type: ProviderTypeOpenAICompat, Model: "gpt-4o", BaseURL: "https://api.openai.com/v1", APIKey: os.Getenv("OPENAI_API_KEY")},
		"openrouter":  {Type: ProviderTypeOpenAICompat, Model: "openai/gpt-4o", BaseURL: "https://openrouter.ai/api/v1", APIKey: os.Getenv("OPENROUTER_API_KEY")},
		"deepseek":    {Type: ProviderTypeOpenAICompat, Model: "deepseek-chat", BaseURL: "https://api.deepseek.com/v1", APIKey: os.Getenv("DEEPSEEK_API_KEY")},
		"dashscope":   {Type: ProviderTypeOpenAICompat, Model: "qwen-plus", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", APIKey: os.Getenv("DASHSCOPE_API_KEY")},
		"glm":         {Type: ProviderTypeOpenAICompat, Model: "glm-4.6v", BaseURL: "https://open.bigmodel.cn/api/paas/v4", APIKey: os.Getenv("GLM_API_KEY")},
		"bigmodel":    {Type: ProviderTypeOpenAICompat, Model: "glm-4.6v", BaseURL: "https://open.bigmodel.cn/api/paas/v4", APIKey: os.Getenv("BIGMODEL_API_KEY")},
		"groq":        {Type: ProviderTypeOpenAICompat, Model: "llama-3.3-70b-versatile", BaseURL: "https://api.groq.com/openai/v1", APIKey: os.Getenv("GROQ_API_KEY")},
		"mistral":     {Type: ProviderTypeOpenAICompat, Model: "mistral-large-latest", BaseURL: "https://api.mistral.ai/v1", APIKey: os.Getenv("MISTRAL_API_KEY")},
		"xai":         {Type: ProviderTypeOpenAICompat, Model: "grok-2-latest", BaseURL: "https://api.x.ai/v1", APIKey: os.Getenv("XAI_API_KEY")},
		"minimax":     {Type: ProviderTypeOpenAICompat, Model: "MiniMax-M1", BaseURL: "https://api.minimax.io/v1", APIKey: os.Getenv("MINIMAX_API_KEY")},
		"moonshot":    {Type: ProviderTypeOpenAICompat, Model: "moonshot-v1-8k", BaseURL: "https://api.moonshot.cn/v1", APIKey: os.Getenv("MOONSHOT_API_KEY")},
		"siliconflow": {Type: ProviderTypeOpenAICompat, Model: "Qwen/Qwen2.5-72B-Instruct", BaseURL: "https://api.siliconflow.cn/v1", APIKey: os.Getenv("SILICONFLOW_API_KEY")},
		"together":    {Type: ProviderTypeOpenAICompat, Model: "meta-llama/Llama-3.3-70B-Instruct-Turbo", BaseURL: "https://api.together.xyz/v1", APIKey: os.Getenv("TOGETHER_API_KEY")},
		"fireworks":   {Type: ProviderTypeOpenAICompat, Model: "accounts/fireworks/models/llama-v3p3-70b-instruct", BaseURL: "https://api.fireworks.ai/inference/v1", APIKey: os.Getenv("FIREWORKS_API_KEY")},
		"perplexity":  {Type: ProviderTypeOpenAICompat, Model: "sonar", BaseURL: "https://api.perplexity.ai", APIKey: os.Getenv("PERPLEXITY_API_KEY")},
		"novita":      {Type: ProviderTypeOpenAICompat, Model: "meta-llama/llama-3.1-8b-instruct", BaseURL: "https://api.novita.ai/v3/openai", APIKey: os.Getenv("NOVITA_API_KEY")},
		"ollama":      {Type: ProviderTypeOpenAICompat, Model: "llama3.1", BaseURL: "http://localhost:11434/v1"},
	}
}
