package modelx

import (
	"context"
	"iter"
	"strings"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

type EchoModel struct {
	name string
}

func NewEchoModel(name string) *EchoModel {
	if name == "" {
		name = "echo"
	}
	return &EchoModel{name: name}
}

func (m *EchoModel) Name() string {
	return m.name
}

func (m *EchoModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		text := latestUserText(req)
		if strings.TrimSpace(text) == "" {
			text = "(empty message)"
		}
		resp := &model.LLMResponse{
			Content:      genai.NewContentFromText("Echo: "+text, genai.RoleModel),
			TurnComplete: true,
			ModelVersion: m.name,
		}
		_ = yield(resp, nil)
	}
}

func latestUserText(req *model.LLMRequest) string {
	if req == nil {
		return ""
	}
	for i := len(req.Contents) - 1; i >= 0; i-- {
		content := req.Contents[i]
		if content == nil {
			continue
		}
		if content.Role != "" && content.Role != string(genai.RoleUser) {
			continue
		}
		var b strings.Builder
		for _, part := range content.Parts {
			if part == nil || part.Text == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(part.Text)
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return ""
}
