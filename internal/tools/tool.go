package tools

import (
	"context"
	"encoding/json"

	"miniagent/internal/llm"
)

type Result struct {
	Content string
	IsError bool
	Error   *ToolError
	Events  []Event
}

type Event struct {
	Type string
	Data map[string]any
}

type Permission string

const (
	// PermissionReadOnly means the tool only observes local state. These tools
	// can run automatically because they do not modify files or execute commands.
	PermissionReadOnly Permission = "read_only"

	// PermissionWorkspaceWrite is reserved for tools that create or change files
	// inside the workspace. Day 5 write_file will use this permission.
	PermissionWorkspaceWrite Permission = "workspace_write"

	// PermissionShell is reserved for command execution. It is separated from
	// file writes because shell commands can have much wider side effects.
	PermissionShell Permission = "shell"

	// PermissionAgentState changes MiniAgent's own in-memory harness state. It
	// does not touch the workspace or run commands, so it does not need the same
	// approval boundary as write/shell tools.
	PermissionAgentState Permission = "agent_state"
)

type Tool interface {
	Name() string
	Description() string
	Permission() Permission
	Schema() llm.ToolSchema
	Execute(ctx context.Context, input json.RawMessage) (Result, error)
}

type PreflightTool interface {
	Tool

	// Preflight validates whether a requested tool call could be executed before
	// the harness asks the user for approval. It must not perform side effects.
	Preflight(ctx context.Context, input json.RawMessage) (Result, error)
}
