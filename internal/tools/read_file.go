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
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return Result{Content: "path is required", IsError: true}, nil
	}

	_, target, rel, err := workspacePath(args.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	info, err := os.Stat(target)
	if err != nil {
		return Result{Content: "stat file: " + err.Error(), IsError: true}, nil
	}
	if info.IsDir() {
		return Result{Content: "path is a directory: " + rel, IsError: true}, nil
	}

	maxBytes := clamp(args.MaxBytes, defaultReadMaxBytes, maxReadBytes)
	data, err := os.ReadFile(target)
	if err != nil {
		return Result{Content: "read file: " + err.Error(), IsError: true}, nil
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
