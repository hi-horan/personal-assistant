package modelx

import (
	"testing"

	"personal-assistant/internal/config"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestGLMChatRequestConvertsADKRequest(t *testing.T) {
	temp := float32(0.2)
	topP := float32(0.9)
	maxTokens := int32(1024)
	llm := NewGLMModel(config.Config{
		GLMModel:        "glm-4.6v",
		GLMAPIKey:       "test-key",
		GLMBaseURL:      "https://open.bigmodel.cn/api/paas/v4",
		GLMThinkingType: "enabled",
	}, nil).(*GLMModel)

	req := llm.chatRequest(&model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("hello", genai.RoleUser),
			genai.NewContentFromText("hi", genai.RoleModel),
			genai.NewContentFromText("answer this", genai.RoleUser),
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText("be concise", genai.RoleUser),
			Temperature:       &temp,
			TopP:              &topP,
			MaxOutputTokens:   maxTokens,
			StopSequences:     []string{"STOP"},
		},
	})

	if got, want := req.Model, "glm-4.6v"; got != want {
		t.Fatalf("Model = %q, want %q", got, want)
	}
	if req.Thinking == nil || req.Thinking.Type != "enabled" {
		t.Fatalf("Thinking = %#v, want enabled", req.Thinking)
	}
	if req.Stream {
		t.Fatal("Stream = true, want false for base chat request")
	}
	if req.MaxTokens == nil || *req.MaxTokens != maxTokens {
		t.Fatalf("MaxTokens = %#v, want %d", req.MaxTokens, maxTokens)
	}
	wantMessages := []glmMessage{
		{Role: "system", Content: "be concise"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "user", Content: "answer this"},
	}
	if len(req.Messages) != len(wantMessages) {
		t.Fatalf("len(Messages) = %d, want %d: %#v", len(req.Messages), len(wantMessages), req.Messages)
	}
	for i := range wantMessages {
		if req.Messages[i] != wantMessages[i] {
			t.Fatalf("Messages[%d] = %#v, want %#v", i, req.Messages[i], wantMessages[i])
		}
	}
}

func TestDecodeGLMStreamChunk(t *testing.T) {
	chunk, err := decodeGLMStreamChunk(`{"model":"glm-4.6v","choices":[{"delta":{"content":" hello"}}]}`)
	if err != nil {
		t.Fatalf("decodeGLMStreamChunk() error = %v", err)
	}
	if got, want := chunk.Model, "glm-4.6v"; got != want {
		t.Fatalf("Model = %q, want %q", got, want)
	}
	if got, want := chunk.text(), " hello"; got != want {
		t.Fatalf("text() = %q, want %q", got, want)
	}
}

func TestYieldFinalStreamResponsePreservesWhitespace(t *testing.T) {
	llm := &GLMModel{}
	var got *model.LLMResponse
	llm.yieldFinalStreamResponse(func(resp *model.LLMResponse, err error) bool {
		if err != nil {
			t.Fatalf("yieldFinalStreamResponse() error = %v", err)
		}
		got = resp
		return true
	}, " hello world ", "glm-4.6v")

	if got == nil || got.Content == nil || len(got.Content.Parts) != 1 {
		t.Fatalf("response content = %#v, want one text part", got)
	}
	if got.Content.Parts[0].Text != " hello world " {
		t.Fatalf("final text = %q, want preserved whitespace", got.Content.Parts[0].Text)
	}
}
