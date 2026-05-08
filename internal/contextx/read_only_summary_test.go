package contextx

import (
	"context"
	"strings"
	"testing"

	"miniagent/internal/llm"
)

func TestReadOnlySummaryManagerInjectsExistingSummaryWithoutUpdating(t *testing.T) {
	dir := t.TempDir()
	store := NewFileSummaryStore(dir)
	if err := store.Save(context.Background(), "study", Summary{
		Content:            "# Conversation Summary\n\nexisting summary",
		SummarizedMessages: 10,
		Model:              "summary-test",
	}); err != nil {
		t.Fatal(err)
	}
	manager := ReadOnlySummaryManager{
		Store:          store,
		RecentMessages: 1,
	}
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "planner system"},
		{Role: llm.RoleUser, Content: "old"},
		{Role: llm.RoleUser, Content: "current task"},
	}

	got, err := manager.Build(context.Background(), messages, BuildOptions{SessionID: "study"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("messages = %d, want system + summary + recent", len(got))
	}
	if got[0].Content != "planner system" {
		t.Fatalf("system = %q", got[0].Content)
	}
	if got[1].Role != llm.RoleSystem || !strings.Contains(got[1].Content, "existing summary") {
		t.Fatalf("summary message = %+v", got[1])
	}
	if got[2].Content != "current task" {
		t.Fatalf("recent = %+v, want current task", got[2])
	}

	summary, err := store.Load(context.Background(), "study")
	if err != nil {
		t.Fatal(err)
	}
	if summary.SummarizedMessages != 10 {
		t.Fatalf("summary watermark = %d, want unchanged 10", summary.SummarizedMessages)
	}
}
