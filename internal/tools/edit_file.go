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
		return ErrorResult(ToolError{Type: ErrorValidation, Message: "invalid arguments: " + err.Error(), Recoverable: true, SuggestedNextStep: "Call edit_file again with valid JSON arguments."}), nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: "path is required", Recoverable: true, SuggestedNextStep: "Retry edit_file with a workspace-relative path."}), nil
	}
	if args.OldText == "" {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: "old_text is required", Recoverable: true, SuggestedNextStep: "Read the file first, then retry edit_file with exact old_text."}), nil
	}
	if len([]byte(args.NewText)) > maxEditFileBytes {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: fmt.Sprintf("new_text is too large: max %d bytes", maxEditFileBytes), Recoverable: true, SuggestedNextStep: "Retry with a smaller replacement."}), nil
	}

	_, target, rel, err := workspacePath(args.Path)
	if err != nil {
		return ErrorResult(ToolError{Type: ErrorPathNotAllowed, Message: err.Error(), Recoverable: false, SuggestedNextStep: "Use a path inside the workspace.", Details: map[string]any{"path": args.Path}}), nil
	}
	if rel == "." {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: "path must be a file, got workspace root", Recoverable: true, SuggestedNextStep: "Retry with a file path inside the workspace."}), nil
	}

	info, err := os.Stat(target)
	if err != nil {
		errType := ErrorExecution
		if os.IsNotExist(err) {
			errType = ErrorNotFound
		}
		return ErrorResult(ToolError{Type: errType, Message: "stat file: " + err.Error(), Recoverable: true, SuggestedNextStep: "Use list_files to inspect paths, then retry with an existing file.", Details: map[string]any{"path": rel}}), nil
	}
	if info.IsDir() {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: "path is a directory: " + rel, Recoverable: true, SuggestedNextStep: "Retry with a file path, not a directory.", Details: map[string]any{"path": rel}}), nil
	}
	if info.Size() > maxEditFileBytes {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: fmt.Sprintf("file is too large to edit: max %d bytes", maxEditFileBytes), Recoverable: false, SuggestedNextStep: "Explain that this learning tool refuses large files."}), nil
	}

	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return ErrorResult(ToolError{Type: ErrorExecution, Message: "read file: " + err.Error(), Recoverable: true, SuggestedNextStep: "Retry after checking file permissions or path.", Details: map[string]any{"path": rel}}), nil
	}
	content := string(data)

	// The unique-match rule is the safety core of this tool. It prevents the
	// model from accidentally changing zero places or many similar places.
	matches := strings.Count(content, args.OldText)
	if matches == 0 {
		return ErrorResult(ToolError{Type: ErrorNotFound, Message: "old_text not found in file: " + rel, Recoverable: true, SuggestedNextStep: "Use read_file to inspect the file, then retry edit_file with exact old_text.", Details: map[string]any{"path": rel}}), nil
	}
	if matches > 1 {
		return ErrorResult(ToolError{Type: ErrorNotUnique, Message: fmt.Sprintf("old_text appears %d times; provide a more specific old_text", matches), Recoverable: true, SuggestedNextStep: "Use read_file to inspect the surrounding text, then retry with a larger unique old_text.", Details: map[string]any{"path": rel, "matches": matches}}), nil
	}

	updated := strings.Replace(content, args.OldText, args.NewText, 1)
	if len([]byte(updated)) > maxEditFileBytes {
		return ErrorResult(ToolError{Type: ErrorValidation, Message: fmt.Sprintf("edited file would be too large: max %d bytes", maxEditFileBytes), Recoverable: true, SuggestedNextStep: "Retry with a smaller replacement."}), nil
	}
	if err := os.WriteFile(target, []byte(updated), info.Mode().Perm()); err != nil {
		return ErrorResult(ToolError{Type: ErrorExecution, Message: "write file: " + err.Error(), Recoverable: true, SuggestedNextStep: "Check file permissions or choose a writable file.", Details: map[string]any{"path": rel}}), nil
	}

	return Result{Content: fmt.Sprintf("edited file: %s (replaced 1 occurrence)", rel)}, nil
}
