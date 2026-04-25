package store

import (
	"strings"

	"google.golang.org/genai"
)

func contentText(content *genai.Content) string {
	if content == nil {
		return ""
	}
	parts := make([]string, 0, len(content.Parts))
	for _, part := range content.Parts {
		if part == nil || part.Text == "" {
			continue
		}
		parts = append(parts, part.Text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func compactWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
