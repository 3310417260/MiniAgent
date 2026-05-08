package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"miniagent/internal/llm"
)

const maxWriteBytes = 100000

type WriteFileTool struct{}

func (WriteFileTool) Name() string {
	return "write_file"
}

func (WriteFileTool) Description() string {
	return "Write a UTF-8 text file inside the workspace."
}

func (WriteFileTool) Permission() Permission {
	return PermissionWorkspaceWrite
}

func (WriteFileTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        "write_file",
		Description: "Write a UTF-8 text file inside the workspace. Requires explicit approval before execution. By default it refuses to overwrite existing files.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path relative to the workspace root.",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Full file content to write.",
				},
				"overwrite": map[string]any{
					"type":        "boolean",
					"description": "Whether to overwrite an existing file. Defaults to false.",
				},
			},
			"required": []string{"path", "content"},
		},
	}
}

func (WriteFileTool) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	var args struct {
		Path      string `json:"path"`
		Content   string `json:"content"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := json.Unmarshal(defaultJSON(input), &args); err != nil {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: "invalid arguments: " + err.Error(), Recoverable: true, SuggestedNextStep: "Call write_file again with valid JSON arguments."}), nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: "path is required", Recoverable: true, SuggestedNextStep: "Retry write_file with a workspace-relative file path."}), nil
	}
	if len([]byte(args.Content)) > maxWriteBytes {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: fmt.Sprintf("content is too large: max %d bytes", maxWriteBytes), Recoverable: true, SuggestedNextStep: "Retry with smaller file content."}), nil
	}

	_, target, rel, err := workspacePath(args.Path)
	if err != nil {
		return ErrorResult(ToolError{Type: ErrorPathNotAllowed, Message: err.Error(), Recoverable: false, SuggestedNextStep: "Use a path inside the workspace.", Details: map[string]any{"path": args.Path}}), nil
	}
	if rel == "." {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: "path must be a file, got workspace root", Recoverable: true, SuggestedNextStep: "Retry with a file path inside the workspace."}), nil
	}

	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	if info, err := os.Stat(target); err == nil {
		if info.IsDir() {
			return ErrorResult(ToolError{Type: ErrorValidation, Message: "path is a directory: " + rel, Recoverable: true, SuggestedNextStep: "Retry with a file path, not a directory.", Details: map[string]any{"path": rel}}), nil
		}
		if !args.Overwrite {
			return ErrorResult(ToolError{Type: ErrorValidation, Message: "file already exists; set overwrite=true to replace: " + rel, Recoverable: true, SuggestedNextStep: "Use read_file to inspect the file, then retry with overwrite=true only if replacing it is intended.", Details: map[string]any{"path": rel}}), nil
		}
	} else if !os.IsNotExist(err) {
		return ErrorResult(ToolError{Type: ErrorExecution, Message: "stat file: " + err.Error(), Recoverable: true, SuggestedNextStep: "Check the target path and retry.", Details: map[string]any{"path": rel}}), nil
	}

	parent := filepath.Dir(target)
	if info, err := os.Stat(parent); err != nil {
		return ErrorResult(ToolError{Type: ErrorNotFound, Message: "parent directory does not exist: " + filepath.ToSlash(filepath.Dir(rel)), Recoverable: true, SuggestedNextStep: "Choose an existing directory or create the parent directory first.", Details: map[string]any{"path": rel}}), nil
	} else if !info.IsDir() {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: "parent path is not a directory: " + filepath.ToSlash(filepath.Dir(rel)), Recoverable: true, SuggestedNextStep: "Choose a path whose parent is a directory.", Details: map[string]any{"path": rel}}), nil
	}

	if err := os.WriteFile(target, []byte(args.Content), 0o644); err != nil {
		return ErrorResult(ToolError{Type: ErrorExecution, Message: "write file: " + err.Error(), Recoverable: true, SuggestedNextStep: "Check permissions or choose a writable path.", Details: map[string]any{"path": rel}}), nil
	}

	return Result{Content: fmt.Sprintf("wrote file: %s (%d bytes)", rel, len([]byte(args.Content)))}, nil
}
