package config

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const DefaultEmbeddingDimension = 1024

const DefaultInstruction = `你是一个简洁、可靠的个人助手。
请优先使用当前会话历史、会话摘要和长期记忆中与问题相关的信息。
不要编造记忆；如果记忆缺失或不确定，请基于用户当前消息直接回答。
当用户表达稳定偏好、个人资料、长期事实或值得后续使用的信息时，调用 memory_save 保存。
如果提供的记忆上下文不足，可以使用 load memory 工具搜索记忆。

记忆上下文：
{rag_context?}`

type Config struct {
	HTTPAddr            string     `yaml:"http_addr"`
	AppName             string     `yaml:"app_name"`
	PrintConfig         bool       `yaml:"printconfig"`
	Instruction         string     `yaml:"instruction"`
	DatabaseURL         string     `yaml:"database_url"`
	LogLevel            slog.Level `yaml:"-"`
	LogLevelText        string     `yaml:"log_level"`
	ModelProvider       string     `yaml:"model_provider"`
	GeminiModel         string     `yaml:"gemini_model"`
	GeminiAPIKey        string     `yaml:"gemini_api_key"`
	GLMModel            string     `yaml:"glm_model"`
	GLMAPIKey           string     `yaml:"glm_api_key"`
	GLMBaseURL          string     `yaml:"glm_base_url"`
	GLMThinkingType     string     `yaml:"glm_thinking_type"`
	BigModelAPIKey      string     `yaml:"bigmodel_api_key"`
	BigModelBaseURL     string     `yaml:"bigmodel_base_url"`
	EmbeddingProvider   string     `yaml:"embedding_provider"`
	EmbeddingModel      string     `yaml:"embedding_model"`
	EmbeddingDimension  int        `yaml:"embedding_dimension"`
	OTelServiceName     string     `yaml:"otel_service_name"`
	OTelEndpoint        string     `yaml:"otel_exporter_otlp_endpoint"`
	OTelMetricsEndpoint string     `yaml:"otel_metrics_endpoint"`
	MCP                 MCPConfig  `yaml:"mcp"`
}

type MCPConfig struct {
	Servers []MCPServer `json:"servers" yaml:"servers"`
}

type MCPServer struct {
	Name                string            `json:"name" yaml:"name"`
	Command             string            `json:"command" yaml:"command"`
	Args                []string          `json:"args" yaml:"args"`
	Env                 map[string]string `json:"env" yaml:"env"`
	RequireConfirmation bool              `json:"require_confirmation" yaml:"require_confirmation"`
}

func LoadFile(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, fmt.Errorf("config path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	cfg := Config{
		HTTPAddr:           ":8080",
		AppName:            "personal-assistant",
		Instruction:        DefaultInstruction,
		LogLevelText:       "info",
		ModelProvider:      "echo",
		GeminiModel:        "gemini-2.5-flash",
		GLMModel:           "glm-4.6v",
		GLMBaseURL:         "https://open.bigmodel.cn/api/paas/v4",
		BigModelBaseURL:    "https://open.bigmodel.cn/api/paas/v4",
		EmbeddingProvider:  "hash",
		EmbeddingModel:     "text-embedding-004",
		EmbeddingDimension: DefaultEmbeddingDimension,
		OTelServiceName:    "personal-assistant",
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Config{}, fmt.Errorf("parse config %q: multiple YAML documents are not supported", path)
	}
	cfg.LogLevelText = normalizeLogLevelText(cfg.LogLevelText)
	cfg.LogLevel = parseLogLevel(cfg.LogLevelText)
	cfg.ModelProvider = strings.ToLower(strings.TrimSpace(cfg.ModelProvider))
	cfg.EmbeddingProvider = strings.ToLower(strings.TrimSpace(cfg.EmbeddingProvider))
	cfg.HTTPAddr = strings.TrimSpace(cfg.HTTPAddr)
	cfg.AppName = strings.TrimSpace(cfg.AppName)
	cfg.Instruction = strings.TrimSpace(cfg.Instruction)
	cfg.DatabaseURL = strings.TrimSpace(cfg.DatabaseURL)
	cfg.GeminiModel = strings.TrimSpace(cfg.GeminiModel)
	cfg.GeminiAPIKey = strings.TrimSpace(cfg.GeminiAPIKey)
	cfg.GLMModel = strings.TrimSpace(cfg.GLMModel)
	cfg.GLMAPIKey = strings.TrimSpace(cfg.GLMAPIKey)
	cfg.GLMBaseURL = strings.TrimRight(strings.TrimSpace(cfg.GLMBaseURL), "/")
	cfg.GLMThinkingType = strings.ToLower(strings.TrimSpace(cfg.GLMThinkingType))
	cfg.BigModelAPIKey = strings.TrimSpace(cfg.BigModelAPIKey)
	cfg.BigModelBaseURL = strings.TrimRight(strings.TrimSpace(cfg.BigModelBaseURL), "/")
	cfg.EmbeddingModel = strings.TrimSpace(cfg.EmbeddingModel)
	cfg.OTelServiceName = strings.TrimSpace(cfg.OTelServiceName)
	cfg.OTelEndpoint = strings.TrimSpace(cfg.OTelEndpoint)
	cfg.OTelMetricsEndpoint = strings.TrimSpace(cfg.OTelMetricsEndpoint)
	if cfg.OTelMetricsEndpoint == "" {
		cfg.OTelMetricsEndpoint = cfg.OTelEndpoint
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("database_url is required")
	}
	if cfg.AppName == "" {
		return Config{}, fmt.Errorf("app_name is required")
	}
	if utf8.RuneCountInString(cfg.AppName) > 256 {
		return Config{}, fmt.Errorf("app_name must be at most 256 characters")
	}
	if cfg.Instruction == "" {
		return Config{}, fmt.Errorf("instruction is required")
	}
	if cfg.ModelProvider != "echo" && cfg.ModelProvider != "gemini" && cfg.ModelProvider != "glm" {
		return Config{}, fmt.Errorf("model_provider must be echo, gemini, or glm")
	}
	if cfg.ModelProvider == "gemini" && cfg.GeminiAPIKey == "" {
		return Config{}, fmt.Errorf("gemini_api_key is required when model_provider is gemini")
	}
	if cfg.ModelProvider == "glm" && cfg.GLMAPIKey == "" {
		return Config{}, fmt.Errorf("glm_api_key is required when model_provider is glm")
	}
	if cfg.ModelProvider == "glm" && cfg.GLMModel == "" {
		return Config{}, fmt.Errorf("glm_model is required when model_provider is glm")
	}
	if cfg.ModelProvider == "glm" && cfg.GLMBaseURL == "" {
		return Config{}, fmt.Errorf("glm_base_url is required when model_provider is glm")
	}
	if cfg.GLMThinkingType != "" && cfg.GLMThinkingType != "enabled" && cfg.GLMThinkingType != "disabled" {
		return Config{}, fmt.Errorf("glm_thinking_type must be empty, enabled, or disabled")
	}
	if cfg.EmbeddingProvider != "hash" && cfg.EmbeddingProvider != "gemini" && cfg.EmbeddingProvider != "bigmodel" {
		return Config{}, fmt.Errorf("embedding_provider must be hash, gemini, or bigmodel")
	}
	if cfg.EmbeddingProvider == "gemini" && cfg.GeminiAPIKey == "" {
		return Config{}, fmt.Errorf("gemini_api_key is required when embedding_provider is gemini")
	}
	if cfg.EmbeddingProvider == "bigmodel" && cfg.BigModelAPIKey == "" {
		return Config{}, fmt.Errorf("bigmodel_api_key is required when embedding_provider is bigmodel")
	}
	if cfg.EmbeddingProvider == "bigmodel" && cfg.BigModelBaseURL == "" {
		return Config{}, fmt.Errorf("bigmodel_base_url is required when embedding_provider is bigmodel")
	}
	if cfg.EmbeddingDimension != DefaultEmbeddingDimension {
		return Config{}, fmt.Errorf("embedding_dimension must be %s for the current pgvector schema", strconv.Itoa(DefaultEmbeddingDimension))
	}
	if cfg.EmbeddingModel == "" {
		return Config{}, fmt.Errorf("embedding_model is required")
	}
	if err := validateMCPConfig(cfg.MCP); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func PrintEffective(w io.Writer, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal effective config: %w", err)
	}
	if _, err := fmt.Fprintln(w, "--- effective config ---"); err != nil {
		return fmt.Errorf("write effective config header: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write effective config: %w", err)
	}
	if _, err := fmt.Fprintln(w, "--- end effective config ---"); err != nil {
		return fmt.Errorf("write effective config footer: %w", err)
	}
	return nil
}

func validateMCPConfig(cfg MCPConfig) error {
	for i, server := range cfg.Servers {
		if strings.TrimSpace(server.Name) == "" {
			return fmt.Errorf("mcp server %d missing name", i)
		}
		if strings.TrimSpace(server.Command) == "" {
			return fmt.Errorf("mcp server %q missing command", server.Name)
		}
	}
	return nil
}

func parseLogLevel(value string) slog.Level {
	switch normalizeLogLevelText(value) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func normalizeLogLevelText(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return "debug"
	case "warn", "warning":
		return "warn"
	case "error":
		return "error"
	default:
		return "info"
	}
}
