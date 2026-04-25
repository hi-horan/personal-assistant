package config

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultEmbeddingDimension = 768

type Config struct {
	HTTPAddr            string     `yaml:"http_addr"`
	AppName             string     `yaml:"app_name"`
	DatabaseURL         string     `yaml:"database_url"`
	LogLevel            slog.Level `yaml:"-"`
	LogLevelText        string     `yaml:"log_level"`
	ModelProvider       string     `yaml:"model_provider"`
	GeminiModel         string     `yaml:"gemini_model"`
	GeminiAPIKey        string     `yaml:"gemini_api_key"`
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
		LogLevelText:       "info",
		ModelProvider:      "echo",
		GeminiModel:        "gemini-2.5-flash",
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
	cfg.LogLevel = parseLogLevel(cfg.LogLevelText)
	cfg.ModelProvider = strings.ToLower(strings.TrimSpace(cfg.ModelProvider))
	cfg.EmbeddingProvider = strings.ToLower(strings.TrimSpace(cfg.EmbeddingProvider))
	cfg.HTTPAddr = strings.TrimSpace(cfg.HTTPAddr)
	cfg.AppName = strings.TrimSpace(cfg.AppName)
	cfg.DatabaseURL = strings.TrimSpace(cfg.DatabaseURL)
	cfg.GeminiModel = strings.TrimSpace(cfg.GeminiModel)
	cfg.GeminiAPIKey = strings.TrimSpace(cfg.GeminiAPIKey)
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
	if cfg.ModelProvider != "echo" && cfg.ModelProvider != "gemini" {
		return Config{}, fmt.Errorf("model_provider must be echo or gemini")
	}
	if cfg.ModelProvider == "gemini" && cfg.GeminiAPIKey == "" {
		return Config{}, fmt.Errorf("gemini_api_key is required when model_provider is gemini")
	}
	if cfg.EmbeddingProvider != "hash" && cfg.EmbeddingProvider != "gemini" {
		return Config{}, fmt.Errorf("embedding_provider must be hash or gemini")
	}
	if cfg.EmbeddingProvider == "gemini" && cfg.GeminiAPIKey == "" {
		return Config{}, fmt.Errorf("gemini_api_key is required when embedding_provider is gemini")
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
	switch strings.ToLower(strings.TrimSpace(value)) {
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
