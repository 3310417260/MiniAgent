package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"miniagent/internal/agent"
	"miniagent/internal/contextx"
	"miniagent/internal/llm"
	"miniagent/internal/prompt"
	"miniagent/internal/tools"
)

type fakeLoopClient struct {
	responses []llm.GenerateResponse
	requests  []llm.GenerateRequest
}

func (f *fakeLoopClient) Generate(ctx context.Context, req llm.GenerateRequest) (llm.GenerateResponse, error) {
	f.requests = append(f.requests, cloneGenerateRequest(req))
	if len(f.responses) == 0 {
		return llm.GenerateResponse{}, errors.New("no fake response left")
	}

	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *fakeLoopClient) GenerateStream(ctx context.Context, req llm.GenerateRequest, onDelta func(string)) (llm.GenerateResponse, error) {
	return llm.GenerateResponse{}, errors.New("GenerateStream should not be used by the agent loop")
}

func TestRunAgentLoopExecutesToolAndFeedsResultBack(t *testing.T) {
	toolset := []tools.Tool{tools.TimeTool{}}
	dispatcher := agent.NewDispatcher(toolset)
	call := llm.ToolCall{
		ID:        "call_1",
		Name:      "get_time",
		Arguments: json.RawMessage(`{}`),
	}
	client := &fakeLoopClient{
		responses: []llm.GenerateResponse{
			{
				Assistant: llm.Message{
					Role:      llm.RoleAssistant,
					Content:   "checking time",
					ToolCalls: []llm.ToolCall{call},
				},
				ToolCalls: []llm.ToolCall{call},
			},
			{
				Assistant: llm.Message{
					Role:    llm.RoleAssistant,
					Content: "final answer",
				},
			},
		},
	}

	messages := append(newConversation(prompt.BaseSystemPrompt), llm.Message{
		Role:    llm.RoleUser,
		Content: "what time is it?",
	})

	reply, updated, err := runAgentLoop(context.Background(), client, dispatcher, contextx.RecentNManager{MaxMessages: 40}, toolSchemas(toolset), messages, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Content != "final answer" {
		t.Fatalf("reply.Content = %q, want final answer", reply.Content)
	}
	if len(client.requests) != 2 {
		t.Fatalf("model requests = %d, want 2", len(client.requests))
	}

	secondReqMessages := client.requests[1].Messages
	if len(secondReqMessages) != 4 {
		t.Fatalf("second request messages = %d, want 4", len(secondReqMessages))
	}
	toolMsg := secondReqMessages[3]
	if toolMsg.Role != llm.RoleTool {
		t.Fatalf("tool message role = %s, want %s", toolMsg.Role, llm.RoleTool)
	}
	if toolMsg.ToolCallID != call.ID || toolMsg.ToolName != call.Name {
		t.Fatalf("tool message = %+v, want id=%s name=%s", toolMsg, call.ID, call.Name)
	}
	if len(updated) != 5 {
		t.Fatalf("updated messages = %d, want 5", len(updated))
	}
}

func TestRunAgentLoopTrimsModelMessagesButKeepsFullHistory(t *testing.T) {
	client := &fakeLoopClient{
		responses: []llm.GenerateResponse{
			{
				Assistant: llm.Message{
					Role:    llm.RoleAssistant,
					Content: "final answer",
				},
			},
		},
	}
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "old user"},
		{Role: llm.RoleAssistant, Content: "old assistant"},
		{Role: llm.RoleUser, Content: "new user"},
	}

	reply, updated, err := runAgentLoop(context.Background(), client, agent.NewDispatcher(nil), contextx.RecentNManager{MaxMessages: 1}, nil, messages, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Content != "final answer" {
		t.Fatalf("reply.Content = %q, want final answer", reply.Content)
	}
	if len(client.requests) != 1 {
		t.Fatalf("model requests = %d, want 1", len(client.requests))
	}
	requestMessages := client.requests[0].Messages
	if len(requestMessages) != 2 {
		t.Fatalf("request messages = %d, want system + recent message", len(requestMessages))
	}
	if requestMessages[0].Role != llm.RoleSystem || requestMessages[1].Content != "new user" {
		t.Fatalf("request messages = %+v, want system and newest user", requestMessages)
	}
	if len(updated) != 5 {
		t.Fatalf("updated messages = %d, want full history plus reply", len(updated))
	}
}

func cloneGenerateRequest(req llm.GenerateRequest) llm.GenerateRequest {
	req.Messages = append([]llm.Message(nil), req.Messages...)
	req.Tools = append([]llm.ToolSchema(nil), req.Tools...)
	return req
}
