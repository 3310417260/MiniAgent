package agent

import (
	"context"
	"encoding/json"

	"miniagent/internal/tools"
)

type ApprovalRequest struct {
	ToolName        string
	ToolDescription string
	Permission      tools.Permission
	Arguments       json.RawMessage
}

// ApprovalFunc is the harness boundary between "the model requested a tool"
// and "MiniAgent is allowed to actually run it". A CLI can ask the user, while
// tests or future configs can approve/deny automatically.
type ApprovalFunc func(ctx context.Context, req ApprovalRequest) (bool, error)

func requiresApproval(permission tools.Permission) bool {
	return permission == tools.PermissionWorkspaceWrite || permission == tools.PermissionShell
}
