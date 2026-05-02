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

func (d *Dispatcher) Execute(ctx context.Context, call llm.ToolCall) (tools.Result, bool, error) {
	tool, ok := d.tools[call.Name]
	if !ok {
		return tools.Result{}, false, nil
	}

	result, err := tool.Execute(ctx, call.Arguments)
	if err != nil {
		return tools.Result{}, true, err
	}

	return result, true, nil
}
