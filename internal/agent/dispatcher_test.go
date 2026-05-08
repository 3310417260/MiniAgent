package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"miniagent/internal/llm"
	"miniagent/internal/tools"
)

type fakePreflightShellTool struct {
	executed bool
}

func (t *fakePreflightShellTool) Name() string {
	return "fake_shell"
}

func (t *fakePreflightShellTool) Description() string {
	return "Fake shell tool for preflight tests."
}

func (t *fakePreflightShellTool) Permission() tools.Permission {
	return tools.PermissionShell
}

func (t *fakePreflightShellTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{Name: t.Name()}
}

func (t *fakePreflightShellTool) Preflight(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	return tools.Result{Content: "preflight rejected", IsError: true}, nil
}

func (t *fakePreflightShellTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	t.executed = true
	return tools.Result{Content: "executed"}, nil
}

func TestDispatcherPreflightRunsBeforeApproval(t *testing.T) {
	tool := &fakePreflightShellTool{}
	dispatcher := NewDispatcher([]tools.Tool{tool})
	approvalCalls := 0

	result, found, err := dispatcher.Execute(context.Background(), llm.ToolCall{
		ID:        "call_preflight",
		Name:      "fake_shell",
		Arguments: json.RawMessage(`{}`),
	}, func(ctx context.Context, req ApprovalRequest) (bool, error) {
		approvalCalls++
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected tool to be found")
	}
	if !result.IsError || !strings.Contains(result.Content, "preflight rejected") {
		t.Fatalf("result = %+v, want preflight error", result)
	}
	if result.Error == nil || result.Error.Type != tools.ErrorValidation || !result.Error.Recoverable {
		t.Fatalf("tool error = %+v, want recoverable validation error", result.Error)
	}
	if approvalCalls != 0 {
		t.Fatalf("approval calls = %d, want 0", approvalCalls)
	}
	if tool.executed {
		t.Fatal("tool executed after preflight rejection")
	}
}

func TestDispatcherApprovalDeniedReturnsStructuredError(t *testing.T) {
	tool := &fakeApprovalTool{}
	dispatcher := NewDispatcher([]tools.Tool{tool})

	result, found, err := dispatcher.Execute(context.Background(), llm.ToolCall{
		ID:        "call_denied",
		Name:      "fake_write",
		Arguments: json.RawMessage(`{}`),
	}, func(ctx context.Context, req ApprovalRequest) (bool, error) {
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected tool to be found")
	}
	if !result.IsError || result.Error == nil {
		t.Fatalf("result = %+v, want structured error", result)
	}
	if result.Error.Type != tools.ErrorPermissionDenied || result.Error.Recoverable {
		t.Fatalf("tool error = %+v, want unrecoverable permission_denied", result.Error)
	}
}
