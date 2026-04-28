package config

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	HTTPAddr    string `yaml:"http_addr"`
	AppName     string `yaml:"app_name"`
	PrintConfig bool   `yaml:"printconfig"`
	Instruction string `yaml:"instruction"`
	DatabaseURL string `yaml:"database_url"`
	LogLevel    string `yaml:"log_level"`

	ModelProvider string                    `yaml:"model_provider"`
	Providers     map[string]ProviderConfig `yaml:"providers"`

	GeminiModel  string `yaml:"gemini_model"`
	GeminiAPIKey string `yaml:"gemini_api_key"`

	GLMModel        string `yaml:"glm_model"`
	GLMAPIKey       string `yaml:"glm_api_key"`
	GLMBaseURL      string `yaml:"glm_base_url"`
	GLMThinkingType string `yaml:"glm_thinking_type"`

	BigModelAPIKey  string `yaml:"bigmodel_api_key"`
	BigModelBaseURL string `yaml:"bigmodel_base_url"`

	EmbeddingProvider  string `yaml:"embedding_provider"`
	EmbeddingModel     string `yaml:"embedding_model"`
	EmbeddingDimension int    `yaml:"embedding_dimension"`

	OTelServiceName          string `yaml:"otel_service_name"`
	OTelExporterOTLPEndpoint string `yaml:"otel_exporter_otlp_endpoint"`
	OTelMetricsEndpoint      string `yaml:"otel_metrics_endpoint"`

	MCP MCPConfig `yaml:"mcp"`
}

type ProviderConfig struct {
	Type            string            `yaml:"type"`
	Model           string            `yaml:"model"`
	APIKey          string            `yaml:"api_key"`
	BaseURL         string            `yaml:"base_url"`
	APIPath         string            `yaml:"api_path"`
	Enabled         *bool             `yaml:"enabled"`
	Headers         map[string]string `yaml:"headers"`
	TimeoutSeconds  int               `yaml:"timeout_seconds"`
	ReasoningEffort string            `yaml:"reasoning_effort"`
	ThinkingType    string            `yaml:"thinking_type"`
	ServiceTier     string            `yaml:"service_tier"`
}

type MCPConfig struct {
	Servers []MCPServerConfig `yaml:"servers"`
}

type MCPServerConfig struct {
	Name                string            `yaml:"name"`
	Command             string            `yaml:"command"`
	Args                []string          `yaml:"args"`
	Env                 map[string]string `yaml:"env"`
	RequireConfirmation bool              `yaml:"require_confirmation"`
}

func Load(path string) (*Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func Defaults() *Config {
	return &Config{
		HTTPAddr:           ":8080",
		AppName:            "personal-assistant",
		LogLevel:           "info",
		ModelProvider:      "echo",
		GeminiModel:        "gemini-2.5-flash",
		GLMModel:           "glm-4.6v",
		GLMBaseURL:         "https://open.bigmodel.cn/api/paas/v4",
		BigModelBaseURL:    "https://open.bigmodel.cn/api/paas/v4",
		EmbeddingProvider:  "hash",
		EmbeddingModel:     "text-embedding-004",
		EmbeddingDimension: 1024,
		OTelServiceName:    "personal-assistant",
	}
}

func (c *Config) applyDefaults() {
	d := Defaults()
	if c.HTTPAddr == "" {
		c.HTTPAddr = d.HTTPAddr
	}
	if c.AppName == "" {
		c.AppName = d.AppName
	}
	if c.LogLevel == "" {
		c.LogLevel = d.LogLevel
	}
	if c.ModelProvider == "" {
		c.ModelProvider = d.ModelProvider
	}
	if c.Providers == nil {
		c.Providers = make(map[string]ProviderConfig)
	}
	if c.GeminiModel == "" {
		c.GeminiModel = d.GeminiModel
	}
	if c.GLMModel == "" {
		c.GLMModel = d.GLMModel
	}
	if c.GLMBaseURL == "" {
		c.GLMBaseURL = d.GLMBaseURL
	}
	if c.BigModelBaseURL == "" {
		c.BigModelBaseURL = d.BigModelBaseURL
	}
	if c.EmbeddingProvider == "" {
		c.EmbeddingProvider = d.EmbeddingProvider
	}
	if c.EmbeddingModel == "" {
		c.EmbeddingModel = d.EmbeddingModel
	}
	if c.EmbeddingDimension == 0 {
		c.EmbeddingDimension = d.EmbeddingDimension
	}
	if c.OTelServiceName == "" {
		c.OTelServiceName = d.OTelServiceName
	}
	if c.OTelMetricsEndpoint == "" {
		c.OTelMetricsEndpoint = c.OTelExporterOTLPEndpoint
	}
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.HTTPAddr) == "" {
		return fmt.Errorf("http_addr is required")
	}
	if strings.TrimSpace(c.AppName) == "" {
		return fmt.Errorf("app_name is required")
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log_level must be one of debug, info, warn, error")
	}
	if c.EmbeddingDimension < 0 {
		return fmt.Errorf("embedding_dimension must be non-negative")
	}
	for i, srv := range c.MCP.Servers {
		if strings.TrimSpace(srv.Name) == "" {
			return fmt.Errorf("mcp.servers[%d].name is required", i)
		}
		if strings.TrimSpace(srv.Command) == "" {
			return fmt.Errorf("mcp.servers[%d].command is required", i)
		}
	}
	for name, provider := range c.Providers {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("providers contains an empty provider name")
		}
		if provider.TimeoutSeconds < 0 {
			return fmt.Errorf("providers.%s.timeout_seconds must be non-negative", name)
		}
	}
	return nil
}

func (c *Config) Redacted() map[string]any {
	return map[string]any{
		"http_addr":                   c.HTTPAddr,
		"app_name":                    c.AppName,
		"printconfig":                 c.PrintConfig,
		"database_url":                redactURL(c.DatabaseURL),
		"log_level":                   c.LogLevel,
		"model_provider":              c.ModelProvider,
		"providers":                   c.redactedProviders(),
		"gemini_model":                c.GeminiModel,
		"gemini_api_key":              redactSecret(c.GeminiAPIKey),
		"glm_model":                   c.GLMModel,
		"glm_api_key":                 redactSecret(c.GLMAPIKey),
		"glm_base_url":                c.GLMBaseURL,
		"glm_thinking_type":           c.GLMThinkingType,
		"bigmodel_api_key":            redactSecret(c.BigModelAPIKey),
		"bigmodel_base_url":           c.BigModelBaseURL,
		"embedding_provider":          c.EmbeddingProvider,
		"embedding_model":             c.EmbeddingModel,
		"embedding_dimension":         c.EmbeddingDimension,
		"otel_service_name":           c.OTelServiceName,
		"otel_exporter_otlp_endpoint": c.OTelExporterOTLPEndpoint,
		"otel_metrics_endpoint":       c.OTelMetricsEndpoint,
		"mcp_server_count":            len(c.MCP.Servers),
	}
}

func (c *Config) redactedProviders() map[string]map[string]any {
	out := make(map[string]map[string]any, len(c.Providers))
	for name, provider := range c.Providers {
		out[name] = map[string]any{
			"type":             provider.Type,
			"model":            provider.Model,
			"api_key":          redactSecret(provider.APIKey),
			"base_url":         provider.BaseURL,
			"api_path":         provider.APIPath,
			"timeout_seconds":  provider.TimeoutSeconds,
			"reasoning_effort": provider.ReasoningEffort,
			"thinking_type":    provider.ThinkingType,
			"service_tier":     provider.ServiceTier,
		}
	}
	return out
}

func redactSecret(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "***"
	}
	return value[:4] + "***" + value[len(value)-4:]
}

func redactURL(value string) string {
	if value == "" {
		return ""
	}
	at := strings.LastIndex(value, "@")
	scheme := strings.Index(value, "://")
	if at <= scheme {
		return value
	}
	return value[:scheme+3] + "***:***" + value[at:]
}
