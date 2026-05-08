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

	var preflightEvents []tools.Event
	// Some tools can cheaply prove that a request is invalid before asking the
	// user for approval. This avoids prompting for calls that cannot run anyway,
	// such as a skill script outside the allowlist.
	if preflightTool, ok := tool.(tools.PreflightTool); ok {
		result, err := preflightTool.Preflight(ctx, call.Arguments)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return tools.Result{}, true, ctxErr
			}
			return tools.ErrorResult(tools.ExecutionToolError("preflight failed: " + err.Error())), true, nil
		}
		preflightEvents = append(preflightEvents, result.Events...)
		if result.IsError {
			result = tools.EnsureErrorResult(result, tools.ToolError{
				Type:              tools.ErrorValidation,
				Message:           result.Content,
				Recoverable:       true,
				SuggestedNextStep: "Read the error message, correct the tool arguments, and retry if appropriate.",
			})
			return result, true, nil
		}
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
			result := tools.ErrorResult(tools.ToolError{
				Type:              tools.ErrorPermissionDenied,
				Message:           "tool call requires approval but no approver is configured: " + tool.Name(),
				Recoverable:       false,
				SuggestedNextStep: "Explain that this action cannot run without an approval boundary.",
				Details: map[string]any{
					"tool":       tool.Name(),
					"permission": tool.Permission(),
				},
			})
			result.Events = append(preflightEvents, result.Events...)
			return result, true, nil
		}

		allowed, err := approve(ctx, req)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return tools.Result{}, true, ctxErr
			}
			result := tools.ErrorResult(tools.ExecutionToolError("approval failed: " + err.Error()))
			result.Events = append(preflightEvents, result.Events...)
			return result, true, nil
		}
		if !allowed {
			result := tools.ErrorResult(tools.ToolError{
				Type:              tools.ErrorPermissionDenied,
				Message:           "tool call denied by user: " + tool.Name(),
				Recoverable:       false,
				SuggestedNextStep: "Stop trying to execute this action and explain that the user denied approval.",
				Details: map[string]any{
					"tool":       tool.Name(),
					"permission": tool.Permission(),
				},
			})
			result.Events = append(preflightEvents, result.Events...)
			return result, true, nil
		}
	}

	result, err := tool.Execute(ctx, call.Arguments)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return tools.Result{}, true, ctxErr
		}
		result = tools.ErrorResult(tools.ExecutionToolError("execute tool failed: " + err.Error()))
	}
	if result.IsError {
		result = tools.EnsureErrorResult(result, tools.ToolError{
			Type:              tools.ErrorExecution,
			Message:           result.Content,
			Recoverable:       true,
			SuggestedNextStep: "Use the error message to correct the tool call or choose a safer inspection tool.",
		})
	}
	result.Events = append(preflightEvents, result.Events...)

	return result, true, nil
}
