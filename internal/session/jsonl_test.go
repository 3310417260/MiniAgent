package session

import (
	"context"
	"testing"

	"miniagent/internal/llm"
)

func TestJSONLStoreAppendLoadListAndClear(t *testing.T) {
	ctx := context.Background()
	store := NewJSONLStore(t.TempDir())

	if err := store.Append(ctx, "study", llm.Message{Role: llm.RoleUser, Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ctx, "study", llm.Message{Role: llm.RoleAssistant, Content: "hi"}); err != nil {
		t.Fatal(err)
	}

	messages, err := store.Load(ctx, "study")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	if messages[0].Role != llm.RoleUser || messages[0].Content != "hello" {
		t.Fatalf("first message = %+v", messages[0])
	}

	ids, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "study" {
		t.Fatalf("ids = %v, want [study]", ids)
	}

	if err := store.Clear(ctx, "study"); err != nil {
		t.Fatal(err)
	}
	messages, err = store.Load(ctx, "study")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("messages after clear = %d, want 0", len(messages))
	}
}

func TestJSONLStoreRejectsInvalidSessionID(t *testing.T) {
	store := NewJSONLStore(t.TempDir())
	if err := store.Append(context.Background(), "../secret", llm.Message{Role: llm.RoleUser, Content: "x"}); err == nil {
		t.Fatal("Append accepted invalid session id")
	}
}
