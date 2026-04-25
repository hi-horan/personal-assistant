package modelx

import (
	"context"
	"iter"
	"strings"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

type EchoModel struct{}

func NewEchoModel() model.LLM {
	return EchoModel{}
}

func (EchoModel) Name() string {
	return "echo"
}

func (EchoModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		text := lastUserText(req)
		if text == "" {
			text = "No user message was provided."
		}
		_ = yield(&model.LLMResponse{
			Content:      genai.NewContentFromText("echo: "+text, genai.RoleModel),
			TurnComplete: true,
			ModelVersion: "echo",
		}, nil)
	}
}

func lastUserText(req *model.LLMRequest) string {
	if req == nil {
		return ""
	}
	for i := len(req.Contents) - 1; i >= 0; i-- {
		content := req.Contents[i]
		if content == nil || content.Role != genai.RoleUser {
			continue
		}
		parts := make([]string, 0, len(content.Parts))
		for _, part := range content.Parts {
			if part != nil && part.Text != "" {
				parts = append(parts, part.Text)
			}
		}
		if len(parts) > 0 {
			return strings.TrimSpace(strings.Join(parts, "\n"))
		}
	}
	return ""
}
