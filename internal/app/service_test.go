package app

import (
	"testing"

	"google.golang.org/genai"
)

func TestContentTextPreservesStreamingWhitespace(t *testing.T) {
	content := &genai.Content{
		Parts: []*genai.Part{
			{Text: "hello"},
			{Text: " world"},
		},
	}

	got := contentText(content)
	want := "hello\n world"
	if got != want {
		t.Fatalf("contentText() = %q, want %q", got, want)
	}
}
