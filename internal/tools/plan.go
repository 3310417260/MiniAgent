package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"miniagent/internal/llm"
	"miniagent/internal/plan"
)

type SetPlanTool struct {
	State *plan.State
}

func (SetPlanTool) Name() string {
	return "set_plan"
}

func (SetPlanTool) Description() string {
	return "Set the current task plan state."
}

func (SetPlanTool) Permission() Permission {
	return PermissionAgentState
}

func (SetPlanTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        "set_plan",
		Description: "Set the current task plan before execution. Use this to propose or revise a plan for user approval.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{
					"type":        "string",
					"description": "Short title for the task plan.",
				},
				"steps": map[string]any{
					"type":        "array",
					"description": "Ordered plan steps. Keep this concise and actionable.",
					"items": map[string]any{
						"type": "string",
					},
				},
			},
			"required": []string{"title", "steps"},
		},
	}
}

func (t SetPlanTool) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	if t.State == nil {
		return Result{Content: "plan state is not configured", IsError: true}, nil
	}

	var args struct {
		Title string   `json:"title"`
		Steps []string `json:"steps"`
	}
	if err := json.Unmarshal(defaultJSON(input), &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	if strings.TrimSpace(args.Title) == "" {
		return Result{Content: "title is required", IsError: true}, nil
	}
	if len(args.Steps) == 0 {
		return Result{Content: "steps are required", IsError: true}, nil
	}

	t.State.Set(args.Title, args.Steps)
	return Result{Content: "plan set:\n" + t.State.String()}, nil
}

type UpdatePlanTool struct {
	State *plan.State
}

func (UpdatePlanTool) Name() string {
	return "update_plan"
}

func (UpdatePlanTool) Description() string {
	return "Update one step in the current task plan state."
}

func (UpdatePlanTool) Permission() Permission {
	return PermissionAgentState
}

func (UpdatePlanTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        "update_plan",
		Description: "Update one step in the current task plan. Use it to mark steps pending, in_progress, done, or failed.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"index": map[string]any{
					"type":        "integer",
					"description": "1-based step index.",
				},
				"status": map[string]any{
					"type":        "string",
					"description": "New step status.",
					"enum":        []string{"pending", "in_progress", "done", "failed"},
				},
				"text": map[string]any{
					"type":        "string",
					"description": "Optional replacement step text.",
				},
			},
			"required": []string{"index", "status"},
		},
	}
}

func (t UpdatePlanTool) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	if t.State == nil {
		return Result{Content: "plan state is not configured", IsError: true}, nil
	}

	var args struct {
		Index  int    `json:"index"`
		Status string `json:"status"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal(defaultJSON(input), &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}

	status := plan.StepStatus(args.Status)
	if err := t.State.Update(args.Index, status, args.Text); err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	return Result{Content: fmt.Sprintf("plan step %d updated to %s\n%s", args.Index, status, t.State.String())}, nil
}
