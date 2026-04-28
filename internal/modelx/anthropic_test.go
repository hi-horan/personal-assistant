package modelx

import (
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestAnthropicMessageRequest(t *testing.T) {
	m := NewAnthropicModel(AnthropicConfig{
		Model:           "claude-test",
		ReasoningEffort: "medium",
	})
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("你好", genai.RoleUser),
			genai.NewContentFromText("你好", genai.RoleModel),
			genai.NewContentFromText("继续", genai.RoleUser),
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText("你是助手", genai.RoleUser),
			MaxOutputTokens:   2048,
		},
	}

	body := m.messageRequest(req, true)
	if body.Model != "claude-test" {
		t.Fatalf("model = %q", body.Model)
	}
	if body.System != "你是助手" {
		t.Fatalf("system = %q", body.System)
	}
	if len(body.Messages) != 3 {
		t.Fatalf("messages len = %d", len(body.Messages))
	}
	if body.Messages[1].Role != "assistant" {
		t.Fatalf("assistant role = %q", body.Messages[1].Role)
	}
	if body.MaxTokens != 2048 {
		t.Fatalf("max tokens = %d", body.MaxTokens)
	}
	if body.Thinking == nil || body.Thinking.BudgetTokens != 4096 {
		t.Fatalf("thinking = %+v", body.Thinking)
	}
}
