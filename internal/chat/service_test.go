package chat

import (
	"context"
	"testing"

	"personal-assistant/internal/modelx"
)

func TestServiceChatEcho(t *testing.T) {
	ctx := context.Background()
	svc, err := NewService(Config{
		AppName:       "test",
		Instruction:   "你是一个测试助手。",
		Model:         modelx.NewEchoModel("echo"),
		ModelProvider: "echo",
		ModelName:     "echo",
	})
	if err != nil {
		t.Fatal(err)
	}

	sess, err := svc.CreateSession(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Chat(ctx, sess.ID, "你好")
	if err != nil {
		t.Fatal(err)
	}
	if result.Message.Content != "Echo: 你好" {
		t.Fatalf("assistant content = %q", result.Message.Content)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(result.Messages))
	}
}
