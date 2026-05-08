package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"miniagent/internal/llm"
)

const (
	defaultReadMaxBytes = 12000
	maxReadBytes        = 50000
)

type ReadFileTool struct{}

func (ReadFileTool) Name() string {
	return "read_file"
}

func (ReadFileTool) Description() string {
	return "Read a UTF-8 text file from the workspace."
}

func (ReadFileTool) Permission() Permission {
	return PermissionReadOnly
}

func (ReadFileTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        "read_file",
		Description: "Read a UTF-8 text file from the workspace. Use this when you know the file path and need its content.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path relative to the workspace root.",
				},
				"max_bytes": map[string]any{
					"type":        "integer",
					"description": "Maximum bytes to read. Defaults to 12000.",
				},
			},
			"required": []string{"path"},
		},
	}
}

func (ReadFileTool) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	var args struct {
		Path     string `json:"path"`
		MaxBytes int    `json:"max_bytes"`
	}
	if err := json.Unmarshal(defaultJSON(input), &args); err != nil {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: "invalid arguments: " + err.Error(), Recoverable: true, SuggestedNextStep: "Call read_file again with valid JSON arguments."}), nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: "path is required", Recoverable: true, SuggestedNextStep: "Retry read_file with a workspace-relative path."}), nil
	}

	_, target, rel, err := workspacePath(args.Path)
	if err != nil {
		return ErrorResult(ToolError{Type: ErrorPathNotAllowed, Message: err.Error(), Recoverable: false, SuggestedNextStep: "Use a path inside the workspace.", Details: map[string]any{"path": args.Path}}), nil
	}
	info, err := os.Stat(target)
	if err != nil {
		errType := ErrorExecution
		if os.IsNotExist(err) {
			errType = ErrorNotFound
		}
		return ErrorResult(ToolError{Type: errType, Message: "stat file: " + err.Error(), Recoverable: true, SuggestedNextStep: "Use list_files to inspect available paths, then retry read_file.", Details: map[string]any{"path": rel}}), nil
	}
	if info.IsDir() {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: "path is a directory: " + rel, Recoverable: true, SuggestedNextStep: "Use list_files for directories or retry read_file with a file path.", Details: map[string]any{"path": rel}}), nil
	}

	maxBytes := clamp(args.MaxBytes, defaultReadMaxBytes, maxReadBytes)
	data, err := os.ReadFile(target)
	if err != nil {
		return ErrorResult(ToolError{Type: ErrorExecution, Message: "read file: " + err.Error(), Recoverable: true, SuggestedNextStep: "Check file permissions or retry with a different file.", Details: map[string]any{"path": rel}}), nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	truncated := false
	if len(data) > maxBytes {
		data = data[:maxBytes]
		truncated = true
	}

	content := string(data)
	if truncated {
		content += fmt.Sprintf("\n\n... truncated after %d bytes", maxBytes)
	}
	return Result{Content: fmt.Sprintf("file: %s\n%s", rel, content)}, nil
}
