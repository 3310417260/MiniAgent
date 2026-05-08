package contextx

import (
	"context"
	"errors"
	"strings"
	"testing"

	"miniagent/internal/llm"
)

type fakeSummaryClient struct {
	requests []llm.GenerateRequest
}

func (f *fakeSummaryClient) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	f.requests = append(f.requests, req)
	return llm.GenerateResponse{
		Assistant: llm.Message{
			Role:    llm.RoleAssistant,
			Content: "# Conversation Summary\n\n## User Profile\n- user is learning context engineering",
		},
	}, nil
}

func (f *fakeSummaryClient) GenerateStream(ctx context.Context, req llm.GenerateRequest, onDelta func(string)) (llm.GenerateResponse, error) {
	return llm.GenerateResponse{}, nil
}

type fakeFailSummaryClient struct{}

func (f fakeFailSummaryClient) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	return llm.GenerateResponse{}, errors.New("summary unavailable")
}

func (f fakeFailSummaryClient) GenerateStream(ctx context.Context, req llm.GenerateRequest, onDelta func(string)) (llm.GenerateResponse, error) {
	return llm.GenerateResponse{}, nil
}

func TestRollingSummaryManagerSummarizesOlderMessages(t *testing.T) {
	dir := t.TempDir()
	store := NewFileSummaryStore(dir)
	client := &fakeSummaryClient{}
	manager := RollingSummaryManager{
		Client:           client,
		Store:            store,
		RecentMessages:   2,
		TriggerMessages:  4,
		KeepMessages:     2,
		BatchMessages:    1,
		MaxSummaryChars:  3000,
		SummaryModelName: "summary-test",
	}
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "one"},
		{Role: llm.RoleAssistant, Content: "two"},
		{Role: llm.RoleUser, Content: "three"},
		{Role: llm.RoleAssistant, Content: "four"},
		{Role: llm.RoleUser, Content: "five"},
	}

	got, err := manager.Build(context.Background(), messages, BuildOptions{SessionID: "study"})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("summary requests = %d, want 1", len(client.requests))
	}
	if len(got) != 4 {
		t.Fatalf("model messages = %d, want system + summary + 2 recent", len(got))
	}
	if got[1].Role != llm.RoleSystem || !strings.Contains(got[1].Content, "Conversation Summary") {
		t.Fatalf("summary message = %+v", got[1])
	}
	if got[2].Content != "four" || got[3].Content != "five" {
		t.Fatalf("recent messages = %+v, want four/five", got[2:])
	}

	summary, err := store.Load(context.Background(), "study")
	if err != nil {
		t.Fatal(err)
	}
	if summary.SummarizedMessages != 3 {
		t.Fatalf("summarized messages = %d, want 3", summary.SummarizedMessages)
	}
	if summary.Model != "summary-test" {
		t.Fatalf("summary model = %q, want summary-test", summary.Model)
	}
}

func TestRollingSummaryManagerDoesNotResummarizeSameRange(t *testing.T) {
	dir := t.TempDir()
	store := NewFileSummaryStore(dir)
	client := &fakeSummaryClient{}
	manager := RollingSummaryManager{
		Client:          client,
		Store:           store,
		RecentMessages:  2,
		TriggerMessages: 4,
		KeepMessages:    2,
		BatchMessages:   1,
	}
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "one"},
		{Role: llm.RoleAssistant, Content: "two"},
		{Role: llm.RoleUser, Content: "three"},
		{Role: llm.RoleAssistant, Content: "four"},
		{Role: llm.RoleUser, Content: "five"},
	}

	if _, err := manager.Build(context.Background(), messages, BuildOptions{SessionID: "study"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Build(context.Background(), messages, BuildOptions{SessionID: "study"}); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("summary requests = %d, want 1", len(client.requests))
	}
}

func TestRollingSummaryManagerWaitsForBatchSize(t *testing.T) {
	dir := t.TempDir()
	store := NewFileSummaryStore(dir)
	client := &fakeSummaryClient{}
	manager := RollingSummaryManager{
		Client:          client,
		Store:           store,
		RecentMessages:  2,
		TriggerMessages: 4,
		KeepMessages:    2,
		BatchMessages:   4,
	}
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "one"},
		{Role: llm.RoleAssistant, Content: "two"},
		{Role: llm.RoleUser, Content: "three"},
		{Role: llm.RoleAssistant, Content: "four"},
		{Role: llm.RoleUser, Content: "five"},
	}

	got, err := manager.Build(context.Background(), messages, BuildOptions{SessionID: "study"})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("summary requests = %d, want 0 before batch threshold", len(client.requests))
	}
	if len(got) != 3 {
		t.Fatalf("model messages = %d, want system + 2 recent without summary", len(got))
	}
	if got[1].Content != "four" || got[2].Content != "five" {
		t.Fatalf("recent messages = %+v, want four/five", got[1:])
	}
}

func TestRollingSummaryManagerFallsBackWhenSummaryFails(t *testing.T) {
	dir := t.TempDir()
	store := NewFileSummaryStore(dir)
	manager := RollingSummaryManager{
		Client:          fakeFailSummaryClient{},
		Store:           store,
		RecentMessages:  2,
		TriggerMessages: 4,
		KeepMessages:    2,
		BatchMessages:   1,
	}
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "one"},
		{Role: llm.RoleAssistant, Content: "two"},
		{Role: llm.RoleUser, Content: "three"},
		{Role: llm.RoleAssistant, Content: "four"},
		{Role: llm.RoleUser, Content: "five"},
	}

	got, err := manager.Build(context.Background(), messages, BuildOptions{SessionID: "study"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("model messages = %d, want system + 2 recent fallback", len(got))
	}
	if got[1].Content != "four" || got[2].Content != "five" {
		t.Fatalf("recent messages = %+v, want four/five", got[1:])
	}

	summary, err := store.Load(context.Background(), "study")
	if err != nil {
		t.Fatal(err)
	}
	if summary.SummarizedMessages != 0 || summary.Content != "" {
		t.Fatalf("summary = %+v, want unchanged after failed update", summary)
	}
}
