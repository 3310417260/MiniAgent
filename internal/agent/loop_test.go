package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"miniagent/internal/contextx"
	"miniagent/internal/llm"
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

type fakeApprovalTool struct {
	executed bool
}

func (t *fakeApprovalTool) Name() string {
	return "fake_write"
}

func (t *fakeApprovalTool) Description() string {
	return "Fake write tool for approval tests."
}

func (t *fakeApprovalTool) Permission() tools.Permission {
	return tools.PermissionWorkspaceWrite
}

func (t *fakeApprovalTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *fakeApprovalTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	t.executed = true
	return tools.Result{Content: "fake write executed"}, nil
}

func TestRunExecutesToolAndFeedsResultBack(t *testing.T) {
	toolset := []tools.Tool{tools.TimeTool{}}
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
	runtime := &Agent{
		Client:         client,
		Dispatcher:     NewDispatcher(toolset),
		ContextManager: contextx.RecentNManager{MaxMessages: 40},
		Tools:          toolset,
		MaxTurns:       8,
	}

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "what time is it?"},
	}

	result, err := runtime.Run(context.Background(), messages, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Assistant.Content != "final answer" {
		t.Fatalf("reply.Content = %q, want final answer", result.Assistant.Content)
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
	if len(result.Messages) != 5 {
		t.Fatalf("updated messages = %d, want 5", len(result.Messages))
	}
	if len(result.ToolResults) != 1 {
		t.Fatalf("tool results = %d, want 1", len(result.ToolResults))
	}
	if result.ToolResults[0].ToolName != "get_time" || result.ToolResults[0].IsError {
		t.Fatalf("tool result = %+v, want successful get_time", result.ToolResults[0])
	}
}

func TestRunDeniesApprovalRequiredToolWithoutApprover(t *testing.T) {
	tool := &fakeApprovalTool{}
	toolset := []tools.Tool{tool}
	call := llm.ToolCall{
		ID:        "call_approval",
		Name:      "fake_write",
		Arguments: json.RawMessage(`{}`),
	}
	client := &fakeLoopClient{
		responses: []llm.GenerateResponse{
			{
				Assistant: llm.Message{
					Role:      llm.RoleAssistant,
					Content:   "need write",
					ToolCalls: []llm.ToolCall{call},
				},
				ToolCalls: []llm.ToolCall{call},
			},
			{
				Assistant: llm.Message{
					Role:    llm.RoleAssistant,
					Content: "write was denied",
				},
			},
		},
	}
	runtime := &Agent{
		Client:     client,
		Dispatcher: NewDispatcher(toolset),
		Tools:      toolset,
		MaxTurns:   8,
	}

	result, err := runtime.Run(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "write"}}, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if tool.executed {
		t.Fatal("tool executed without approval")
	}
	if result.Assistant.Content != "write was denied" {
		t.Fatalf("reply.Content = %q, want denial-aware final response", result.Assistant.Content)
	}
	if len(client.requests) != 2 {
		t.Fatalf("model requests = %d, want 2", len(client.requests))
	}
	secondReqMessages := client.requests[1].Messages
	toolMsg := secondReqMessages[len(secondReqMessages)-1]
	if toolMsg.Role != llm.RoleTool || !strings.Contains(toolMsg.Content, "requires approval") {
		t.Fatalf("tool message = %+v, want approval error result", toolMsg)
	}
	if len(result.ToolResults) != 1 || !result.ToolResults[0].IsError {
		t.Fatalf("tool results = %+v, want one error result", result.ToolResults)
	}
}

func TestRunApprovesApprovalRequiredTool(t *testing.T) {
	tool := &fakeApprovalTool{}
	toolset := []tools.Tool{tool}
	call := llm.ToolCall{
		ID:        "call_approval",
		Name:      "fake_write",
		Arguments: json.RawMessage(`{"path":"demo.txt"}`),
	}
	client := &fakeLoopClient{
		responses: []llm.GenerateResponse{
			{
				Assistant: llm.Message{
					Role:      llm.RoleAssistant,
					Content:   "need write",
					ToolCalls: []llm.ToolCall{call},
				},
				ToolCalls: []llm.ToolCall{call},
			},
			{
				Assistant: llm.Message{
					Role:    llm.RoleAssistant,
					Content: "write finished",
				},
			},
		},
	}
	runtime := &Agent{
		Client:     client,
		Dispatcher: NewDispatcher(toolset),
		Tools:      toolset,
		MaxTurns:   8,
	}
	var approvalReq ApprovalRequest

	result, err := runtime.Run(context.Background(), []llm.Message{{Role: llm.RoleUser, Content: "write"}}, RunOptions{
		ApproveTool: func(ctx context.Context, req ApprovalRequest) (bool, error) {
			approvalReq = req
			return true, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !tool.executed {
		t.Fatal("tool was not executed after approval")
	}
	if approvalReq.ToolName != "fake_write" || approvalReq.Permission != tools.PermissionWorkspaceWrite {
		t.Fatalf("approval request = %+v, want fake_write workspace_write", approvalReq)
	}
	if result.Assistant.Content != "write finished" {
		t.Fatalf("reply.Content = %q, want write finished", result.Assistant.Content)
	}
}

func TestRunTrimsModelMessagesButKeepsFullHistory(t *testing.T) {
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
	runtime := &Agent{
		Client:         client,
		Dispatcher:     NewDispatcher(nil),
		ContextManager: contextx.RecentNManager{MaxMessages: 1},
		MaxTurns:       8,
	}
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "old user"},
		{Role: llm.RoleAssistant, Content: "old assistant"},
		{Role: llm.RoleUser, Content: "new user"},
	}

	result, err := runtime.Run(context.Background(), messages, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Assistant.Content != "final answer" {
		t.Fatalf("reply.Content = %q, want final answer", result.Assistant.Content)
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
	if len(result.Messages) != 5 {
		t.Fatalf("updated messages = %d, want full history plus reply", len(result.Messages))
	}
}

func cloneGenerateRequest(req llm.GenerateRequest) llm.GenerateRequest {
	req.Messages = append([]llm.Message(nil), req.Messages...)
	req.Tools = append([]llm.ToolSchema(nil), req.Tools...)
	return req
}
