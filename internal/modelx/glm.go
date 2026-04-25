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

	"personal-assistant/internal/config"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

const glmChatCompletionsPath = "/chat/completions"

type GLMModel struct {
	client       *http.Client
	model        string
	apiKey       string
	baseURL      string
	thinkingType string
	logger       *slog.Logger
}

func NewGLMModel(cfg config.Config, logger *slog.Logger) model.LLM {
	return &GLMModel{
		client:       &http.Client{Timeout: 90 * time.Second},
		model:        cfg.GLMModel,
		apiKey:       cfg.GLMAPIKey,
		baseURL:      strings.TrimRight(cfg.GLMBaseURL, "/"),
		thinkingType: cfg.GLMThinkingType,
		logger:       logger,
	}
}

func (m *GLMModel) Name() string {
	return m.model
}

func (m *GLMModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if stream {
			m.generateStream(ctx, req, yield)
			return
		}
		resp, err := m.generate(ctx, req)
		yield(resp, err)
	}
}

func (m *GLMModel) generate(ctx context.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
	payload := m.chatRequest(req)
	payload.Stream = false
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal glm request: %w", err)
	}
	m.logRequest(ctx, false, body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+glmChatCompletionsPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create glm request: %w", err)
	}
	httpReq.Header.Set("authorization", "Bearer "+m.apiKey)
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "application/json")

	httpResp, err := m.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call glm chat completions: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read glm response: %w", err)
	}
	m.logResponse(ctx, false, httpResp.StatusCode, respBody)
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, fmt.Errorf("glm chat completions failed: status=%d body=%s", httpResp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded glmChatResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode glm response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("glm response has no choices")
	}
	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if content == "" {
		return nil, fmt.Errorf("glm response choice has empty content")
	}

	return &model.LLMResponse{
		Content:      genai.NewContentFromText(content, genai.RoleModel),
		TurnComplete: true,
		ModelVersion: decoded.Model,
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(decoded.Usage.PromptTokens),
			CandidatesTokenCount: int32(decoded.Usage.CompletionTokens),
			TotalTokenCount:      int32(decoded.Usage.TotalTokens),
		},
	}, nil
}

func (m *GLMModel) generateStream(ctx context.Context, req *model.LLMRequest, yield func(*model.LLMResponse, error) bool) {
	payload := m.chatRequest(req)
	payload.Stream = true
	body, err := json.Marshal(payload)
	if err != nil {
		yield(nil, fmt.Errorf("marshal glm stream request: %w", err))
		return
	}
	m.logRequest(ctx, true, body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+glmChatCompletionsPath, bytes.NewReader(body))
	if err != nil {
		yield(nil, fmt.Errorf("create glm stream request: %w", err))
		return
	}
	httpReq.Header.Set("authorization", "Bearer "+m.apiKey)
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")

	httpResp, err := m.client.Do(httpReq)
	if err != nil {
		yield(nil, fmt.Errorf("call glm stream chat completions: %w", err))
		return
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(io.LimitReader(httpResp.Body, 4<<20))
		if readErr != nil {
			yield(nil, fmt.Errorf("read glm stream error response: %w", readErr))
			return
		}
		m.logResponse(ctx, true, httpResp.StatusCode, respBody)
		yield(nil, fmt.Errorf("glm stream chat completions failed: status=%d body=%s", httpResp.StatusCode, strings.TrimSpace(string(respBody))))
		return
	}

	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	var fullText strings.Builder
	modelVersion := m.model
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			m.logStreamChunk(ctx, data)
			m.yieldFinalStreamResponse(yield, fullText.String(), modelVersion)
			return
		}
		m.logStreamChunk(ctx, data)
		chunk, err := decodeGLMStreamChunk(data)
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
			ModelVersion: chunk.Model,
			Partial:      true,
		}, nil) {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		yield(nil, fmt.Errorf("read glm stream response: %w", err))
		return
	}
	if fullText.Len() == 0 {
		yield(nil, fmt.Errorf("glm stream response had no content"))
		return
	}
	m.yieldFinalStreamResponse(yield, fullText.String(), modelVersion)
}

func (m *GLMModel) yieldFinalStreamResponse(yield func(*model.LLMResponse, error) bool, text, modelVersion string) {
	text = strings.TrimSpace(text)
	if text == "" {
		yield(nil, fmt.Errorf("glm stream response had no content"))
		return
	}
	yield(&model.LLMResponse{
		Content:      genai.NewContentFromText(text, genai.RoleModel),
		TurnComplete: true,
		ModelVersion: modelVersion,
	}, nil)
}

func (m *GLMModel) logRequest(ctx context.Context, stream bool, body []byte) {
	if m.logger == nil {
		return
	}
	m.logger.DebugContext(ctx, "glm final request", slog.String("url", m.baseURL+glmChatCompletionsPath), slog.Bool("stream", stream), slog.String("body", string(body)))
}

func (m *GLMModel) logResponse(ctx context.Context, stream bool, status int, body []byte) {
	if m.logger == nil {
		return
	}
	m.logger.DebugContext(ctx, "glm raw response", slog.Bool("stream", stream), slog.Int("status", status), slog.String("body", string(body)))
}

func (m *GLMModel) logStreamChunk(ctx context.Context, data string) {
	if m.logger == nil {
		return
	}
	m.logger.DebugContext(ctx, "glm raw stream chunk", slog.String("data", data))
}

func (m *GLMModel) chatRequest(req *model.LLMRequest) glmChatRequest {
	cfg := generationConfig(req)
	out := glmChatRequest{
		Model:       modelName(req, m.model),
		Messages:    glmMessages(req),
		Temperature: cfg.Temperature,
		TopP:        cfg.TopP,
		MaxTokens:   maxOutputTokens(cfg),
		Stop:        cfg.StopSequences,
	}
	if m.thinkingType != "" {
		out.Thinking = &glmThinking{Type: m.thinkingType}
	}
	return out
}

func glmMessages(req *model.LLMRequest) []glmMessage {
	messages := []glmMessage{}
	if cfg := generationConfig(req); cfg.SystemInstruction != nil {
		if text := contentText(cfg.SystemInstruction); text != "" {
			messages = append(messages, glmMessage{Role: "system", Content: text})
		}
	}
	if req == nil {
		return append(messages, glmMessage{Role: "user", Content: "Continue."})
	}
	for _, content := range req.Contents {
		if text := contentText(content); text != "" {
			messages = append(messages, glmMessage{Role: glmRole(content.Role), Content: text})
		}
	}
	if len(messages) == 0 || messages[len(messages)-1].Role != "user" {
		messages = append(messages, glmMessage{Role: "user", Content: "Continue."})
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

func glmRole(role string) string {
	switch role {
	case genai.RoleModel:
		return "assistant"
	case genai.RoleUser:
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

type glmChatRequest struct {
	Model       string       `json:"model"`
	Messages    []glmMessage `json:"messages"`
	Stream      bool         `json:"stream"`
	Temperature *float32     `json:"temperature,omitempty"`
	TopP        *float32     `json:"top_p,omitempty"`
	MaxTokens   *int32       `json:"max_tokens,omitempty"`
	Stop        []string     `json:"stop,omitempty"`
	Thinking    *glmThinking `json:"thinking,omitempty"`
}

type glmThinking struct {
	Type string `json:"type"`
}

type glmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type glmChatResponse struct {
	Model   string      `json:"model"`
	Choices []glmChoice `json:"choices"`
	Usage   glmUsage    `json:"usage"`
}

type glmChoice struct {
	Message glmMessage `json:"message"`
}

type glmUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type glmStreamChunk struct {
	Model   string            `json:"model"`
	Choices []glmStreamChoice `json:"choices"`
}

func (c glmStreamChunk) text() string {
	if len(c.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(c.Choices[0].Delta.Content)
}

type glmStreamChoice struct {
	Delta glmStreamDelta `json:"delta"`
}

type glmStreamDelta struct {
	Content string `json:"content"`
}

func decodeGLMStreamChunk(data string) (glmStreamChunk, error) {
	var chunk glmStreamChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return glmStreamChunk{}, fmt.Errorf("decode glm stream chunk: %w", err)
	}
	return chunk, nil
}
