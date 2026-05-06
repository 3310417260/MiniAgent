package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"miniagent/internal/llm"
)

const maxEditFileBytes = 200000

type EditFileTool struct{}

func (EditFileTool) Name() string {
	return "edit_file"
}

func (EditFileTool) Description() string {
	return "Edit a workspace text file by replacing one unique old_text with new_text."
}

func (EditFileTool) Permission() Permission {
	return PermissionWorkspaceWrite
}

func (EditFileTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        "edit_file",
		Description: "Edit a workspace text file by replacing one unique old_text with new_text. Requires explicit approval before execution. The old_text must appear exactly once.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path relative to the workspace root.",
				},
				"old_text": map[string]any{
					"type":        "string",
					"description": "Exact text to replace. It must appear exactly once in the file.",
				},
				"new_text": map[string]any{
					"type":        "string",
					"description": "Replacement text.",
				},
			},
			"required": []string{"path", "old_text", "new_text"},
		},
	}
}

func (EditFileTool) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	var args struct {
		Path    string `json:"path"`
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := json.Unmarshal(defaultJSON(input), &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return Result{Content: "path is required", IsError: true}, nil
	}
	if args.OldText == "" {
		return Result{Content: "old_text is required", IsError: true}, nil
	}
	if len([]byte(args.NewText)) > maxEditFileBytes {
		return Result{Content: fmt.Sprintf("new_text is too large: max %d bytes", maxEditFileBytes), IsError: true}, nil
	}

	_, target, rel, err := workspacePath(args.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	if rel == "." {
		return Result{Content: "path must be a file, got workspace root", IsError: true}, nil
	}

	info, err := os.Stat(target)
	if err != nil {
		return Result{Content: "stat file: " + err.Error(), IsError: true}, nil
	}
	if info.IsDir() {
		return Result{Content: "path is a directory: " + rel, IsError: true}, nil
	}
	if info.Size() > maxEditFileBytes {
		return Result{Content: fmt.Sprintf("file is too large to edit: max %d bytes", maxEditFileBytes), IsError: true}, nil
	}

	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return Result{Content: "read file: " + err.Error(), IsError: true}, nil
	}
	content := string(data)

	// The unique-match rule is the safety core of this tool. It prevents the
	// model from accidentally changing zero places or many similar places.
	matches := strings.Count(content, args.OldText)
	if matches == 0 {
		return Result{Content: "old_text not found in file: " + rel, IsError: true}, nil
	}
	if matches > 1 {
		return Result{Content: fmt.Sprintf("old_text appears %d times; provide a more specific old_text", matches), IsError: true}, nil
	}

	updated := strings.Replace(content, args.OldText, args.NewText, 1)
	if len([]byte(updated)) > maxEditFileBytes {
		return Result{Content: fmt.Sprintf("edited file would be too large: max %d bytes", maxEditFileBytes), IsError: true}, nil
	}
	if err := os.WriteFile(target, []byte(updated), info.Mode().Perm()); err != nil {
		return Result{Content: "write file: " + err.Error(), IsError: true}, nil
	}

	return Result{Content: fmt.Sprintf("edited file: %s (replaced 1 occurrence)", rel)}, nil
}
