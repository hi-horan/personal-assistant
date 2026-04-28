package modelx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

const anthropicMessagesPath = "/v1/messages"

type AnthropicConfig struct {
	ProviderName    string
	Model           string
	APIKey          string
	BaseURL         string
	Headers         map[string]string
	Timeout         time.Duration
	ReasoningEffort string
	Logger          *slog.Logger
}

type AnthropicModel struct {
	client          *http.Client
	providerName    string
	model           string
	apiKey          string
	url             string
	headers         map[string]string
	reasoningEffort string
	logger          *slog.Logger
}

func NewAnthropicModel(cfg AnthropicConfig) *AnthropicModel {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &AnthropicModel{
		client:          &http.Client{Timeout: timeout},
		providerName:    firstNonEmpty(cfg.ProviderName, "anthropic"),
		model:           cfg.Model,
		apiKey:          cfg.APIKey,
		url:             baseURL + anthropicMessagesPath,
		headers:         cfg.Headers,
		reasoningEffort: cfg.ReasoningEffort,
		logger:          cfg.Logger,
	}
}

func (m *AnthropicModel) Name() string {
	return m.model
}

func (m *AnthropicModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if stream {
			m.generateStream(ctx, req, yield)
			return
		}
		resp, err := m.generate(ctx, req)
		yield(resp, err)
	}
}

func (m *AnthropicModel) generate(ctx context.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
	payload := m.messageRequest(req, false)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}
	m.logRequest(ctx, false, body)

	httpResp, respBody, err := m.do(ctx, body)
	if err != nil {
		return nil, err
	}
	m.logResponse(ctx, false, httpResp.StatusCode, respBody)
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("anthropic messages failed: status=%d body=%s", httpResp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded anthropicMessageResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("anthropic: decode response: %w", err)
	}
	text := decoded.text()
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("anthropic response has empty content")
	}
	return &model.LLMResponse{
		Content:      genai.NewContentFromText(text, genai.RoleModel),
		TurnComplete: true,
		ModelVersion: firstNonEmpty(decoded.Model, m.model),
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(decoded.Usage.InputTokens),
			CandidatesTokenCount: int32(decoded.Usage.OutputTokens),
			TotalTokenCount:      int32(decoded.Usage.InputTokens + decoded.Usage.OutputTokens),
		},
	}, nil
}

func (m *AnthropicModel) generateStream(ctx context.Context, req *model.LLMRequest, yield func(*model.LLMResponse, error) bool) {
	payload := m.messageRequest(req, true)
	body, err := json.Marshal(payload)
	if err != nil {
		yield(nil, fmt.Errorf("anthropic: marshal stream request: %w", err))
		return
	}
	m.logRequest(ctx, true, body)

	httpReq, err := m.newRequest(ctx, body)
	if err != nil {
		yield(nil, err)
		return
	}
	httpReq.Header.Set("accept", "text/event-stream")
	httpResp, err := m.client.Do(httpReq)
	if err != nil {
		yield(nil, fmt.Errorf("anthropic: call stream messages: %w", err))
		return
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(io.LimitReader(httpResp.Body, 4<<20))
		if readErr != nil {
			yield(nil, fmt.Errorf("anthropic: read stream error response: %w", readErr))
			return
		}
		m.logResponse(ctx, true, httpResp.StatusCode, respBody)
		yield(nil, fmt.Errorf("anthropic stream messages failed: status=%d body=%s", httpResp.StatusCode, strings.TrimSpace(string(respBody))))
		return
	}

	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	var fullText strings.Builder
	modelVersion := m.model
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		m.logStreamChunk(ctx, data)
		var ev anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			yield(nil, fmt.Errorf("anthropic: decode stream event: %w", err))
			return
		}
		if ev.Type == "error" {
			yield(nil, fmt.Errorf("anthropic stream error: %s", ev.Error.Message))
			return
		}
		if ev.Message != nil && ev.Message.Model != "" {
			modelVersion = ev.Message.Model
		}
		if ev.Delta.Text == "" {
			continue
		}
		fullText.WriteString(ev.Delta.Text)
		if !yield(&model.LLMResponse{
			Content:      genai.NewContentFromText(ev.Delta.Text, genai.RoleModel),
			ModelVersion: modelVersion,
			Partial:      true,
		}, nil) {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		yield(nil, fmt.Errorf("anthropic: read stream response: %w", err))
		return
	}
	if strings.TrimSpace(fullText.String()) == "" {
		yield(nil, fmt.Errorf("anthropic stream response had no content"))
		return
	}
	yield(&model.LLMResponse{
		Content:      genai.NewContentFromText(fullText.String(), genai.RoleModel),
		TurnComplete: true,
		ModelVersion: modelVersion,
	}, nil)
}

func (m *AnthropicModel) do(ctx context.Context, body []byte) (*http.Response, []byte, error) {
	httpReq, err := m.newRequest(ctx, body)
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("accept", "application/json")
	httpResp, err := m.client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("anthropic: call messages: %w", err)
	}
	defer httpResp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 4<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("anthropic: read response: %w", err)
	}
	return httpResp, respBody, nil
}

func (m *AnthropicModel) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if m.apiKey != "" {
		httpReq.Header.Set("x-api-key", m.apiKey)
	}
	for k, v := range m.headers {
		if strings.TrimSpace(k) != "" {
			httpReq.Header.Set(k, v)
		}
	}
	return httpReq, nil
}

func (m *AnthropicModel) messageRequest(req *model.LLMRequest, stream bool) anthropicMessageRequest {
	cfg := generationConfig(req)
	out := anthropicMessageRequest{
		Model:       modelName(req, m.model),
		MaxTokens:   1024,
		Messages:    anthropicMessages(req),
		Temperature: cfg.Temperature,
		TopP:        cfg.TopP,
		Stream:      stream,
	}
	if cfg.MaxOutputTokens > 0 {
		out.MaxTokens = cfg.MaxOutputTokens
	}
	if cfg.SystemInstruction != nil {
		out.System = contentText(cfg.SystemInstruction)
	}
	if m.reasoningEffort != "" {
		out.Thinking = anthropicThinkingForEffort(m.reasoningEffort)
	}
	return out
}

func anthropicMessages(req *model.LLMRequest) []anthropicMessage {
	messages := make([]anthropicMessage, 0)
	if req != nil {
		for _, content := range req.Contents {
			if text := contentText(content); text != "" {
				messages = append(messages, anthropicMessage{Role: anthropicRole(content.Role), Content: text})
			}
		}
	}
	if len(messages) == 0 || messages[len(messages)-1].Role != "user" {
		messages = append(messages, anthropicMessage{Role: "user", Content: "Continue."})
	}
	return messages
}

func anthropicRole(role string) string {
	if role == string(genai.RoleModel) {
		return "assistant"
	}
	return "user"
}

func anthropicThinkingForEffort(effort string) *anthropicThinking {
	switch strings.ToLower(effort) {
	case "low":
		return &anthropicThinking{Type: "enabled", BudgetTokens: 1024}
	case "medium":
		return &anthropicThinking{Type: "enabled", BudgetTokens: 4096}
	case "high":
		return &anthropicThinking{Type: "enabled", BudgetTokens: 8192}
	default:
		return nil
	}
}

func (m *AnthropicModel) logRequest(ctx context.Context, stream bool, body []byte) {
	if m.logger == nil {
		return
	}
	m.logger.DebugContext(ctx, "provider request",
		slog.String("provider", m.providerName),
		slog.String("url", m.url),
		slog.Bool("stream", stream),
		slog.String("body", string(body)),
	)
}

func (m *AnthropicModel) logResponse(ctx context.Context, stream bool, status int, body []byte) {
	if m.logger == nil {
		return
	}
	m.logger.DebugContext(ctx, "provider raw response",
		slog.String("provider", m.providerName),
		slog.Bool("stream", stream),
		slog.Int("status", status),
		slog.String("body", string(body)),
	)
}

func (m *AnthropicModel) logStreamChunk(ctx context.Context, data string) {
	if m.logger == nil {
		return
	}
	m.logger.DebugContext(ctx, "provider raw stream chunk",
		slog.String("provider", m.providerName),
		slog.String("data", data),
	)
}

type anthropicMessageRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int32              `json:"max_tokens"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	Temperature *float32           `json:"temperature,omitempty"`
	TopP        *float32           `json:"top_p,omitempty"`
	Stream      bool               `json:"stream"`
	Thinking    *anthropicThinking `json:"thinking,omitempty"`
}

type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicMessageResponse struct {
	Model   string                  `json:"model"`
	Content []anthropicContentBlock `json:"content"`
	Usage   anthropicUsage          `json:"usage"`
}

func (r anthropicMessageResponse) text() string {
	var b strings.Builder
	for _, block := range r.Content {
		if block.Type == "text" && block.Text != "" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicStreamEvent struct {
	Type    string                  `json:"type"`
	Delta   anthropicStreamDelta    `json:"delta"`
	Message *anthropicStreamMessage `json:"message"`
	Error   anthropicStreamError    `json:"error"`
}

type anthropicStreamDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicStreamMessage struct {
	Model string `json:"model"`
}

type anthropicStreamError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
