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

const defaultChatCompletionsPath = "/chat/completions"

type OpenAICompatConfig struct {
	ProviderName    string
	Model           string
	APIKey          string
	BaseURL         string
	APIPath         string
	Headers         map[string]string
	Timeout         time.Duration
	ReasoningEffort string
	ThinkingType    string
	ServiceTier     string
	Logger          *slog.Logger
}

type OpenAICompatModel struct {
	client          *http.Client
	providerName    string
	model           string
	apiKey          string
	url             string
	headers         map[string]string
	reasoningEffort string
	thinkingType    string
	serviceTier     string
	logger          *slog.Logger
}

func NewOpenAICompatModel(cfg OpenAICompatConfig) *OpenAICompatModel {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	return &OpenAICompatModel{
		client:          &http.Client{Timeout: timeout},
		providerName:    cfg.ProviderName,
		model:           cfg.Model,
		apiKey:          cfg.APIKey,
		url:             chatCompletionsURL(cfg.BaseURL, cfg.APIPath),
		headers:         cfg.Headers,
		reasoningEffort: cfg.ReasoningEffort,
		thinkingType:    cfg.ThinkingType,
		serviceTier:     cfg.ServiceTier,
		logger:          cfg.Logger,
	}
}

func (m *OpenAICompatModel) Name() string {
	return m.model
}

func (m *OpenAICompatModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if stream {
			m.generateStream(ctx, req, yield)
			return
		}
		resp, err := m.generate(ctx, req)
		yield(resp, err)
	}
}

func (m *OpenAICompatModel) generate(ctx context.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
	payload := m.chatRequest(req, false)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", m.providerName, err)
	}
	m.logRequest(ctx, false, body)

	httpResp, respBody, err := m.do(ctx, body, "application/json")
	if err != nil {
		return nil, err
	}
	m.logResponse(ctx, false, httpResp.StatusCode, respBody)
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s chat completions failed: status=%d body=%s", m.providerName, httpResp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded openAIChatResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", m.providerName, err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("%s response has no choices", m.providerName)
	}
	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if content == "" {
		return nil, fmt.Errorf("%s response choice has empty content", m.providerName)
	}
	return &model.LLMResponse{
		Content:      genai.NewContentFromText(content, genai.RoleModel),
		TurnComplete: true,
		ModelVersion: firstNonEmpty(decoded.Model, m.model),
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(decoded.Usage.PromptTokens),
			CandidatesTokenCount: int32(decoded.Usage.CompletionTokens),
			TotalTokenCount:      int32(decoded.Usage.TotalTokens),
		},
	}, nil
}

func (m *OpenAICompatModel) generateStream(ctx context.Context, req *model.LLMRequest, yield func(*model.LLMResponse, error) bool) {
	payload := m.chatRequest(req, true)
	body, err := json.Marshal(payload)
	if err != nil {
		yield(nil, fmt.Errorf("%s: marshal stream request: %w", m.providerName, err))
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
		yield(nil, fmt.Errorf("%s: call stream chat completions: %w", m.providerName, err))
		return
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(io.LimitReader(httpResp.Body, 4<<20))
		if readErr != nil {
			yield(nil, fmt.Errorf("%s: read stream error response: %w", m.providerName, readErr))
			return
		}
		m.logResponse(ctx, true, httpResp.StatusCode, respBody)
		yield(nil, fmt.Errorf("%s stream chat completions failed: status=%d body=%s", m.providerName, httpResp.StatusCode, strings.TrimSpace(string(respBody))))
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
		if data == "[DONE]" {
			m.yieldFinalStreamResponse(yield, fullText.String(), modelVersion)
			return
		}
		m.logStreamChunk(ctx, data)
		chunk, err := decodeOpenAIStreamChunk(data)
		if err != nil {
			yield(nil, err)
			return
		}
		text := chunk.text()
		if text == "" {
			continue
		}
		fullText.WriteString(text)
		if chunk.Model != "" {
			modelVersion = chunk.Model
		}
		if !yield(&model.LLMResponse{
			Content:      genai.NewContentFromText(text, genai.RoleModel),
			ModelVersion: firstNonEmpty(chunk.Model, modelVersion),
			Partial:      true,
		}, nil) {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		yield(nil, fmt.Errorf("%s: read stream response: %w", m.providerName, err))
		return
	}
	m.yieldFinalStreamResponse(yield, fullText.String(), modelVersion)
}

func (m *OpenAICompatModel) yieldFinalStreamResponse(yield func(*model.LLMResponse, error) bool, text, modelVersion string) {
	if strings.TrimSpace(text) == "" {
		yield(nil, fmt.Errorf("%s stream response had no content", m.providerName))
		return
	}
	yield(&model.LLMResponse{
		Content:      genai.NewContentFromText(text, genai.RoleModel),
		TurnComplete: true,
		ModelVersion: firstNonEmpty(modelVersion, m.model),
	}, nil)
}

func (m *OpenAICompatModel) do(ctx context.Context, body []byte, accept string) (*http.Response, []byte, error) {
	httpReq, err := m.newRequest(ctx, body)
	if err != nil {
		return nil, nil, err
	}
	httpReq.Header.Set("accept", accept)
	httpResp, err := m.client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: call chat completions: %w", m.providerName, err)
	}
	defer httpResp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 4<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: read response: %w", m.providerName, err)
	}
	return httpResp, respBody, nil
}

func (m *OpenAICompatModel) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", m.providerName, err)
	}
	if m.apiKey != "" {
		httpReq.Header.Set("authorization", "Bearer "+m.apiKey)
	}
	httpReq.Header.Set("content-type", "application/json")
	for k, v := range m.headers {
		if strings.TrimSpace(k) != "" {
			httpReq.Header.Set(k, v)
		}
	}
	return httpReq, nil
}

func (m *OpenAICompatModel) chatRequest(req *model.LLMRequest, stream bool) openAIChatRequest {
	cfg := generationConfig(req)
	out := openAIChatRequest{
		Model:       modelName(req, m.model),
		Messages:    openAIMessages(req),
		Stream:      stream,
		Temperature: cfg.Temperature,
		TopP:        cfg.TopP,
		MaxTokens:   maxOutputTokens(cfg),
		Stop:        cfg.StopSequences,
	}
	if m.reasoningEffort != "" {
		out.ReasoningEffort = m.reasoningEffort
	}
	if m.serviceTier != "" {
		out.ServiceTier = m.serviceTier
	}
	if m.thinkingType != "" {
		out.Thinking = &openAIThinking{Type: m.thinkingType}
	}
	return out
}

func (m *OpenAICompatModel) logRequest(ctx context.Context, stream bool, body []byte) {
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

func (m *OpenAICompatModel) logResponse(ctx context.Context, stream bool, status int, body []byte) {
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

func (m *OpenAICompatModel) logStreamChunk(ctx context.Context, data string) {
	if m.logger == nil {
		return
	}
	m.logger.DebugContext(ctx, "provider raw stream chunk",
		slog.String("provider", m.providerName),
		slog.String("data", data),
	)
}

func openAIMessages(req *model.LLMRequest) []openAIMessage {
	messages := make([]openAIMessage, 0)
	if cfg := generationConfig(req); cfg.SystemInstruction != nil {
		if text := contentText(cfg.SystemInstruction); text != "" {
			messages = append(messages, openAIMessage{Role: "system", Content: text})
		}
	}
	if req != nil {
		for _, content := range req.Contents {
			if text := contentText(content); text != "" {
				messages = append(messages, openAIMessage{Role: openAIRole(content.Role), Content: text})
			}
		}
	}
	if len(messages) == 0 || messages[len(messages)-1].Role != "user" {
		messages = append(messages, openAIMessage{Role: "user", Content: "Continue."})
	}
	return messages
}

func generationConfig(req *model.LLMRequest) *genai.GenerateContentConfig {
	if req == nil || req.Config == nil {
		return &genai.GenerateContentConfig{}
	}
	return req.Config
}

func modelName(req *model.LLMRequest, fallback string) string {
	if req != nil && strings.TrimSpace(req.Model) != "" {
		return strings.TrimSpace(req.Model)
	}
	return fallback
}

func maxOutputTokens(cfg *genai.GenerateContentConfig) *int32 {
	if cfg == nil || cfg.MaxOutputTokens <= 0 {
		return nil
	}
	return &cfg.MaxOutputTokens
}

func openAIRole(role string) string {
	switch role {
	case string(genai.RoleModel):
		return "assistant"
	case string(genai.RoleUser):
		return "user"
	default:
		return "user"
	}
}

func contentText(content *genai.Content) string {
	if content == nil {
		return ""
	}
	parts := make([]string, 0, len(content.Parts))
	for _, part := range content.Parts {
		if part != nil && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, strings.TrimSpace(part.Text))
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func chatCompletionsURL(baseURL, apiPath string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if apiPath == "" {
		apiPath = defaultChatCompletionsPath
	}
	apiPath = "/" + strings.TrimLeft(apiPath, "/")
	if strings.HasSuffix(baseURL, defaultChatCompletionsPath) || strings.HasSuffix(baseURL, apiPath) {
		return baseURL
	}
	return baseURL + apiPath
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type openAIChatRequest struct {
	Model           string          `json:"model"`
	Messages        []openAIMessage `json:"messages"`
	Stream          bool            `json:"stream"`
	Temperature     *float32        `json:"temperature,omitempty"`
	TopP            *float32        `json:"top_p,omitempty"`
	MaxTokens       *int32          `json:"max_tokens,omitempty"`
	Stop            []string        `json:"stop,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	ServiceTier     string          `json:"service_tier,omitempty"`
	Thinking        *openAIThinking `json:"thinking,omitempty"`
}

type openAIThinking struct {
	Type string `json:"type"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Message openAIMessage `json:"message"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIStreamChunk struct {
	Model   string               `json:"model"`
	Choices []openAIStreamChoice `json:"choices"`
}

func (c openAIStreamChunk) text() string {
	if len(c.Choices) == 0 {
		return ""
	}
	return c.Choices[0].Delta.Content
}

type openAIStreamChoice struct {
	Delta openAIStreamDelta `json:"delta"`
}

type openAIStreamDelta struct {
	Content string `json:"content"`
}

func decodeOpenAIStreamChunk(data string) (openAIStreamChunk, error) {
	var chunk openAIStreamChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return openAIStreamChunk{}, fmt.Errorf("decode openai-compatible stream chunk: %w", err)
	}
	return chunk, nil
}
