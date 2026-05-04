package contextx

import (
	"testing"

	"miniagent/internal/llm"
)

func TestRecentNManagerKeepsSystemAndRecentMessages(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "one"},
		{Role: llm.RoleAssistant, Content: "two"},
		{Role: llm.RoleUser, Content: "three"},
	}

	got := RecentNManager{MaxMessages: 2}.Build(messages)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	if got[0].Role != llm.RoleSystem {
		t.Fatalf("first role = %s, want system", got[0].Role)
	}
	if got[1].Content != "two" || got[2].Content != "three" {
		t.Fatalf("messages = %+v, want two and three", got)
	}
}

func TestRecentNManagerWithoutSystem(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "one"},
		{Role: llm.RoleAssistant, Content: "two"},
		{Role: llm.RoleUser, Content: "three"},
	}

	got := RecentNManager{MaxMessages: 1}.Build(messages)
	if len(got) != 1 || got[0].Content != "three" {
		t.Fatalf("messages = %+v, want newest message", got)
	}
}
