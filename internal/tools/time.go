package tools

import (
	"context"
	"encoding/json"
	"time"

	"miniagent/internal/llm"
)

type TimeTool struct{}

func (TimeTool) Name() string {
	return "get_time"
}

func (TimeTool) Description() string {
	return "Get the current local time."
}

func (TimeTool) Permission() Permission {
	return PermissionReadOnly
}

// Schema is the model-facing contract. The model receives this JSON schema in
// the chat request and can decide to return a get_time tool call.
func (TimeTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        "get_time",
		Description: "Get the current local time.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

// Execute is the local implementation. The model never runs this code; it only
// asks for get_time, then MiniAgent dispatches that request here.
func (TimeTool) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	now := time.Now().Format(time.RFC3339)
	return Result{
		Content: now,
		IsError: false,
	}, nil
}
