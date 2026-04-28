package modelx

import (
	"encoding/json"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestOpenAICompatChatRequest(t *testing.T) {
	m := NewOpenAICompatModel(OpenAICompatConfig{
		ProviderName:    "openai",
		Model:           "gpt-test",
		BaseURL:         "https://api.example/v1",
		ReasoningEffort: "high",
	})
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("你好", genai.RoleUser),
			genai.NewContentFromText("你好，有什么可以帮你？", genai.RoleModel),
			genai.NewContentFromText("继续", genai.RoleUser),
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText("你是助手", genai.RoleUser),
			MaxOutputTokens:   123,
		},
	}

	body := m.chatRequest(req, false)
	if body.Model != "gpt-test" {
		t.Fatalf("model = %q", body.Model)
	}
	if len(body.Messages) != 4 {
		t.Fatalf("messages len = %d", len(body.Messages))
	}
	if body.Messages[0].Role != "system" || body.Messages[0].Content != "你是助手" {
		t.Fatalf("system message = %+v", body.Messages[0])
	}
	if body.Messages[2].Role != "assistant" {
		t.Fatalf("assistant role = %q", body.Messages[2].Role)
	}
	if body.MaxTokens == nil || *body.MaxTokens != 123 {
		t.Fatalf("max tokens = %v", body.MaxTokens)
	}
	if body.ReasoningEffort != "high" {
		t.Fatalf("reasoning effort = %q", body.ReasoningEffort)
	}

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatalf("invalid json: %s", data)
	}
}

func TestChatCompletionsURL(t *testing.T) {
	got := chatCompletionsURL("https://api.example/v1/", "")
	if got != "https://api.example/v1/chat/completions" {
		t.Fatalf("url = %q", got)
	}
	got = chatCompletionsURL("https://api.example/v1/chat/completions", "")
	if got != "https://api.example/v1/chat/completions" {
		t.Fatalf("url = %q", got)
	}
}
