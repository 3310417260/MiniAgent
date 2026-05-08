package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"miniagent/internal/contextx"
	"miniagent/internal/llm"
	"miniagent/internal/logx"
	"miniagent/internal/skill"
)

type fakeStreamClient struct {
	streamRequests []llm.GenerateRequest
	generateCalls  int
}

type fakeGenerateClient struct {
	requests []llm.GenerateRequest
}

func (f *fakeGenerateClient) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	f.requests = append(f.requests, cloneGenerateRequest(req))
	return llm.GenerateResponse{
		Assistant: llm.Message{
			Role:    llm.RoleAssistant,
			Content: "dry run guidance",
		},
	}, nil
}

func (f *fakeGenerateClient) GenerateStream(ctx context.Context, req llm.GenerateRequest, onDelta func(string)) (llm.GenerateResponse, error) {
	return llm.GenerateResponse{}, errors.New("GenerateStream should not be used")
}

func (f *fakeStreamClient) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	f.generateCalls++
	return llm.GenerateResponse{}, errors.New("Generate should not be used by chatStream")
}

func (f *fakeStreamClient) GenerateStream(ctx context.Context, req llm.GenerateRequest, onDelta func(string)) (llm.GenerateResponse, error) {
	f.streamRequests = append(f.streamRequests, cloneGenerateRequest(req))
	if onDelta != nil {
		onDelta("hello")
		onDelta(" stream")
	}
	return llm.GenerateResponse{
		Assistant: llm.Message{
			Role:    llm.RoleAssistant,
			Content: "hello stream",
		},
	}, nil
}

func TestChatStreamUsesGenerateStreamWithoutTools(t *testing.T) {
	client := &fakeStreamClient{}
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "old user"},
		{Role: llm.RoleAssistant, Content: "old assistant"},
	}
	var printed string

	reply, updated, err := chatStream(client, contextx.RecentNManager{MaxMessages: 1}, logx.NoopLogger{}, "test", messages, "plain chat", false, func(delta string) {
		printed += delta
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.generateCalls != 0 {
		t.Fatalf("Generate calls = %d, want 0", client.generateCalls)
	}
	if len(client.streamRequests) != 1 {
		t.Fatalf("stream requests = %d, want 1", len(client.streamRequests))
	}
	req := client.streamRequests[0]
	if len(req.Tools) != 0 {
		t.Fatalf("tools = %d, want 0 for plain chat", len(req.Tools))
	}
	if len(req.Messages) != 2 {
		t.Fatalf("request messages = %d, want system + recent user", len(req.Messages))
	}
	if req.Messages[0].Role != llm.RoleSystem || req.Messages[1].Content != "plain chat" {
		t.Fatalf("request messages = %+v, want system and plain chat", req.Messages)
	}
	if reply.Content != "hello stream" || printed != "hello stream" {
		t.Fatalf("reply=%q printed=%q, want streamed assistant text", reply.Content, printed)
	}
	if len(updated) != 5 {
		t.Fatalf("updated messages = %d, want original + user + assistant", len(updated))
	}
}

func TestParseLogCommand(t *testing.T) {
	filter, err := parseLogCommand("/logs", "default")
	if err != nil {
		t.Fatal(err)
	}
	if filter.Session != "default" || filter.All || filter.MaxLines != defaultLogTail {
		t.Fatalf("filter = %+v, want current default session", filter)
	}

	filter, err = parseLogCommand("/logs all type tool_result errors tail 5", "default")
	if err != nil {
		t.Fatal(err)
	}
	if !filter.All || filter.Session != "" || filter.Type != "tool_result" || !filter.ErrorsOnly || filter.MaxLines != 5 {
		t.Fatalf("filter = %+v, want all tool_result errors tail 5", filter)
	}

	filter, err = parseLogCommand("/logs study", "default")
	if err != nil {
		t.Fatal(err)
	}
	if filter.Session != "study" || filter.All {
		t.Fatalf("filter = %+v, want shorthand session study", filter)
	}
}

func TestMatchesLogFilter(t *testing.T) {
	event := logx.Event{
		Type:    "tool_result",
		Session: "study",
		Data: map[string]any{
			"is_error": true,
		},
	}

	if !matchesLogFilter(event, logFilter{Session: "study", Type: "tool_result", ErrorsOnly: true}) {
		t.Fatal("expected matching study tool_result error")
	}
	if matchesLogFilter(event, logFilter{Session: "default"}) {
		t.Fatal("did not expect default session match")
	}
	if !matchesLogFilter(event, logFilter{All: true, ErrorsOnly: true}) {
		t.Fatal("expected all-session error match")
	}
}

func TestLoadSelectedSkillForTaskInjectsOnlySelectedSkillWithoutTools(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "xlsx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "xlsx", "SKILL.md"), []byte(`---
name: xlsx
description: Work with spreadsheets.
---

# XLSX

Use spreadsheet instructions.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	client := &fakeGenerateClient{}
	loaded, reply, err := loadSelectedSkillForTask(client, logx.NoopLogger{}, skill.NewStore(root), skill.Selection{
		Name:   "xlsx",
		Reason: "spreadsheet task",
	}, "整理表格", false, "test")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "xlsx" || reply.Content != "dry run guidance" {
		t.Fatalf("loaded=%+v reply=%+v, want xlsx dry run", loaded, reply)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(client.requests))
	}

	req := client.requests[0]
	if len(req.Tools) != 0 {
		t.Fatalf("tools = %d, want no tools for dry-run skill load", len(req.Tools))
	}
	if len(req.Messages) != 2 {
		t.Fatalf("messages = %d, want system + user", len(req.Messages))
	}
	if !strings.Contains(req.Messages[1].Content, "Loaded SKILL.md body:") ||
		!strings.Contains(req.Messages[1].Content, "Use spreadsheet instructions.") {
		t.Fatalf("user message = %q, want loaded skill body", req.Messages[1].Content)
	}
}

func cloneGenerateRequest(req llm.GenerateRequest) llm.GenerateRequest {
	req.Messages = append([]llm.Message(nil), req.Messages...)
	req.Tools = append([]llm.ToolSchema(nil), req.Tools...)
	return req
}
