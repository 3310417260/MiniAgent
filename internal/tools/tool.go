package tools

import (
	"context"
	"encoding/json"

	"miniagent/internal/llm"
)

type Result struct {
	Content string
	IsError bool
}

type Tool interface {
	Name() string
	Description() string
	Schema() llm.ToolSchema
	Execute(ctx context.Context, input json.RawMessage) (Result, error)
}
