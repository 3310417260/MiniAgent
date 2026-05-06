package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"miniagent/internal/llm"
)

const (
	defaultListMaxEntries = 100
	maxListEntries        = 500
)

type ListFilesTool struct{}

func (ListFilesTool) Name() string {
	return "list_files"
}

func (ListFilesTool) Description() string {
	return "List files and directories under a workspace path."
}

func (ListFilesTool) Permission() Permission {
	return PermissionReadOnly
}

func (ListFilesTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        "list_files",
		Description: "List files and directories under a workspace path. Use this before reading files when you need to inspect project structure.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Directory path relative to the workspace root. Defaults to '.'",
				},
				"max_entries": map[string]any{
					"type":        "integer",
					"description": "Maximum number of entries to return. Defaults to 100.",
				},
				"recursive": map[string]any{
					"type":        "boolean",
					"description": "Whether to recursively list files under subdirectories. Defaults to false.",
				},
			},
		},
	}
}

func (ListFilesTool) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	var args struct {
		Path       string `json:"path"`
		MaxEntries int    `json:"max_entries"`
		Recursive  bool   `json:"recursive"`
	}
	if err := json.Unmarshal(defaultJSON(input), &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}

	root, target, rel, err := workspacePath(args.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	info, err := os.Stat(target)
	if err != nil {
		return Result{Content: "stat path: " + err.Error(), IsError: true}, nil
	}
	if !info.IsDir() {
		return Result{Content: "path is not a directory: " + rel, IsError: true}, nil
	}

	maxEntries := clamp(args.MaxEntries, defaultListMaxEntries, maxListEntries)
	if args.Recursive {
		return listFilesRecursive(ctx, root, target, rel, maxEntries)
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		return Result{Content: "read directory: " + err.Error(), IsError: true}, nil
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})

	lines := []string{
		fmt.Sprintf("workspace: %s", root),
		fmt.Sprintf("directory: %s", rel),
	}
	count := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if shouldSkipName(entry.Name()) {
			continue
		}
		if count >= maxEntries {
			lines = append(lines, fmt.Sprintf("... truncated after %d entries", maxEntries))
			break
		}

		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		lines = append(lines, name)
		count++
	}

	return Result{Content: strings.Join(lines, "\n")}, nil
}

func listFilesRecursive(ctx context.Context, root string, target string, rel string, maxEntries int) (Result, error) {
	lines := []string{
		fmt.Sprintf("workspace: %s", root),
		fmt.Sprintf("directory: %s", rel),
		"recursive: true",
	}
	count := 0
	truncated := false

	err := filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == target {
			return nil
		}
		if entry.IsDir() && shouldSkipName(entry.Name()) {
			return filepath.SkipDir
		}
		if shouldSkipName(entry.Name()) {
			return nil
		}
		if count >= maxEntries {
			truncated = true
			return fs.SkipAll
		}

		entryRel, err := filepath.Rel(root, path)
		if err != nil {
			entryRel = path
		}
		name := filepath.ToSlash(entryRel)
		if entry.IsDir() {
			name += "/"
		}
		lines = append(lines, name)
		count++
		return nil
	})
	if err != nil {
		return Result{Content: "walk directory: " + err.Error(), IsError: true}, nil
	}
	if truncated {
		lines = append(lines, fmt.Sprintf("... truncated after %d entries", maxEntries))
	}

	return Result{Content: strings.Join(lines, "\n")}, nil
}
