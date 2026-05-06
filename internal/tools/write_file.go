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
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return Result{Content: "path is required", IsError: true}, nil
	}
	if len([]byte(args.Content)) > maxWriteBytes {
		return Result{Content: fmt.Sprintf("content is too large: max %d bytes", maxWriteBytes), IsError: true}, nil
	}

	_, target, rel, err := workspacePath(args.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	if rel == "." {
		return Result{Content: "path must be a file, got workspace root", IsError: true}, nil
	}

	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	if info, err := os.Stat(target); err == nil {
		if info.IsDir() {
			return Result{Content: "path is a directory: " + rel, IsError: true}, nil
		}
		if !args.Overwrite {
			return Result{Content: "file already exists; set overwrite=true to replace: " + rel, IsError: true}, nil
		}
	} else if !os.IsNotExist(err) {
		return Result{Content: "stat file: " + err.Error(), IsError: true}, nil
	}

	parent := filepath.Dir(target)
	if info, err := os.Stat(parent); err != nil {
		return Result{Content: "parent directory does not exist: " + filepath.ToSlash(filepath.Dir(rel)), IsError: true}, nil
	} else if !info.IsDir() {
		return Result{Content: "parent path is not a directory: " + filepath.ToSlash(filepath.Dir(rel)), IsError: true}, nil
	}

	if err := os.WriteFile(target, []byte(args.Content), 0o644); err != nil {
		return Result{Content: "write file: " + err.Error(), IsError: true}, nil
	}

	return Result{Content: fmt.Sprintf("wrote file: %s (%d bytes)", rel, len([]byte(args.Content)))}, nil
}
