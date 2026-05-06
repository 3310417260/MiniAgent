package agent

import (
	"context"

	"miniagent/internal/llm"
	"miniagent/internal/tools"
)

type Dispatcher struct {
	tools map[string]tools.Tool
}

func NewDispatcher(toolset []tools.Tool) *Dispatcher {
	index := make(map[string]tools.Tool, len(toolset))
	for _, tool := range toolset {
		index[tool.Name()] = tool
	}

	return &Dispatcher{tools: index}
}

func (d *Dispatcher) Execute(ctx context.Context, call llm.ToolCall, approve ApprovalFunc) (tools.Result, bool, error) {
	tool, ok := d.tools[call.Name]
	if !ok {
		return tools.Result{}, false, nil
	}

	// The model can request a tool, but the harness decides whether it is safe
	// to run. Read-only tools run automatically; write/shell tools must pass an
	// approval boundary before Execute is called.
	if requiresApproval(tool.Permission()) {
		req := ApprovalRequest{
			ToolName:        tool.Name(),
			ToolDescription: tool.Description(),
			Permission:      tool.Permission(),
			Arguments:       call.Arguments,
		}
		if approve == nil {
			return tools.Result{
				Content: "tool call requires approval but no approver is configured: " + tool.Name(),
				IsError: true,
			}, true, nil
		}

		allowed, err := approve(ctx, req)
		if err != nil {
			return tools.Result{}, true, err
		}
		if !allowed {
			return tools.Result{
				Content: "tool call denied by user: " + tool.Name(),
				IsError: true,
			}, true, nil
		}
	}

	result, err := tool.Execute(ctx, call.Arguments)
	if err != nil {
		return tools.Result{}, true, err
	}

	return result, true, nil
}
