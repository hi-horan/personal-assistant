package app

import (
	"fmt"

	"personal-assistant/internal/store"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type saveMemoryArgs struct {
	Content    string         `json:"content" jsonschema_description:"Memory content to save for future conversations."`
	Kind       string         `json:"kind,omitempty" jsonschema_description:"Memory kind, such as semantic, preference, or episodic."`
	Importance float64        `json:"importance,omitempty" jsonschema_description:"Importance from 0.1 to 1.0."`
	Metadata   map[string]any `json:"metadata,omitempty" jsonschema_description:"Optional structured metadata."`
}

type saveMemoryResult struct {
	ID     string `json:"id,omitempty"`
	Saved  bool   `json:"saved"`
	Reason string `json:"reason,omitempty"`
}

func newSaveMemoryTool(memoryStore *store.Store) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:                "memory_save",
		Description:         "Save a stable user preference or durable fact for future conversations.",
		RequireConfirmation: false,
	}, func(ctx tool.Context, args saveMemoryArgs) (saveMemoryResult, error) {
		if args.Content == "" {
			return saveMemoryResult{Saved: false, Reason: "empty content"}, nil
		}
		id, err := memoryStore.SaveMemory(ctx, store.MemoryRecord{
			AppName:    ctx.AppName(),
			UserID:     ctx.UserID(),
			Kind:       args.Kind,
			Content:    args.Content,
			Importance: args.Importance,
			Metadata:   args.Metadata,
		})
		if err != nil {
			return saveMemoryResult{}, fmt.Errorf("save memory: %w", err)
		}
		return saveMemoryResult{ID: id, Saved: id != ""}, nil
	})
}
