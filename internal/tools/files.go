package tools

import (
	"bufio"
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
	defaultReadMaxBytes   = 12000
	maxReadBytes          = 50000
	defaultGrepMaxMatches = 50
	maxGrepMatches        = 200
	maxGrepFileBytes      = 1 << 20
)

type ListFilesTool struct{}

func (ListFilesTool) Name() string {
	return "list_files"
}

func (ListFilesTool) Description() string {
	return "List files and directories under a workspace path."
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

type ReadFileTool struct{}

func (ReadFileTool) Name() string {
	return "read_file"
}

func (ReadFileTool) Description() string {
	return "Read a UTF-8 text file from the workspace."
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

type GrepTextTool struct{}

func (GrepTextTool) Name() string {
	return "grep_text"
}

func (GrepTextTool) Description() string {
	return "Search for literal text in workspace files."
}

func (GrepTextTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        "grep_text",
		Description: "Search for literal text in workspace files. Use this to find identifiers, functions, or phrases.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Literal text to search for. This is not a regular expression.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "File or directory path relative to the workspace root. Defaults to '.'",
				},
				"max_matches": map[string]any{
					"type":        "integer",
					"description": "Maximum number of matches to return. Defaults to 50.",
				},
				"ignore_case": map[string]any{
					"type":        "boolean",
					"description": "Whether to ignore letter case while matching. Defaults to false.",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

func (GrepTextTool) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	var args struct {
		Pattern    string `json:"pattern"`
		Path       string `json:"path"`
		MaxMatches int    `json:"max_matches"`
		IgnoreCase bool   `json:"ignore_case"`
	}
	if err := json.Unmarshal(defaultJSON(input), &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return Result{Content: "pattern is required", IsError: true}, nil
	}

	_, target, rel, err := workspacePath(args.Path)
	if err != nil {
		return Result{Content: err.Error(), IsError: true}, nil
	}
	maxMatches := clamp(args.MaxMatches, defaultGrepMaxMatches, maxGrepMatches)

	var matches []string
	addMatches := func(path string) error {
		fileMatches, err := grepFile(ctx, path, args.Pattern, args.IgnoreCase, maxMatches-len(matches))
		if err != nil {
			return err
		}
		matches = append(matches, fileMatches...)
		return nil
	}

	info, err := os.Stat(target)
	if err != nil {
		return Result{Content: "stat path: " + err.Error(), IsError: true}, nil
	}
	if !info.IsDir() {
		if err := addMatches(target); err != nil {
			return Result{Content: "search file: " + err.Error(), IsError: true}, nil
		}
		return grepResult(rel, args.Pattern, args.IgnoreCase, matches, maxMatches), nil
	}

	err = filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path != target && entry.IsDir() && shouldSkipName(entry.Name()) {
			return filepath.SkipDir
		}
		if entry.IsDir() || shouldSkipName(entry.Name()) {
			return nil
		}
		if len(matches) >= maxMatches {
			return fs.SkipAll
		}
		return addMatches(path)
	})
	if err != nil {
		return Result{Content: "search directory: " + err.Error(), IsError: true}, nil
	}

	return grepResult(rel, args.Pattern, args.IgnoreCase, matches, maxMatches), nil
}

func grepFile(ctx context.Context, path string, pattern string, ignoreCase bool, remaining int) ([]string, error) {
	if remaining <= 0 {
		return nil, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxGrepFileBytes {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var matches []string
	needle := pattern
	if ignoreCase {
		needle = strings.ToLower(needle)
	}
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lineNo++
		line := scanner.Text()
		haystack := line
		if ignoreCase {
			haystack = strings.ToLower(haystack)
		}
		if strings.Contains(haystack, needle) {
			rel, err := filepath.Rel(workspaceRoot(), path)
			if err != nil {
				rel = path
			}
			matches = append(matches, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(rel), lineNo, strings.TrimSpace(line)))
			if len(matches) >= remaining {
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return matches, nil
}

func grepResult(path string, pattern string, ignoreCase bool, matches []string, maxMatches int) Result {
	if len(matches) == 0 {
		return Result{Content: fmt.Sprintf("no matches for %q under %s", pattern, path)}
	}

	lines := []string{
		fmt.Sprintf("matches for %q under %s:", pattern, path),
	}
	if ignoreCase {
		lines = append(lines, "ignore_case: true")
	}
	lines = append(lines, matches...)
	if len(matches) >= maxMatches {
		lines = append(lines, fmt.Sprintf("... stopped after %d matches", maxMatches))
	}
	return Result{Content: strings.Join(lines, "\n")}
}

func defaultJSON(input json.RawMessage) json.RawMessage {
	if len(strings.TrimSpace(string(input))) == 0 {
		return json.RawMessage(`{}`)
	}
	return input
}

func workspaceRoot() string {
	root, err := os.Getwd()
	if err != nil {
		return "."
	}
	return root
}

func workspacePath(requested string) (string, string, string, error) {
	root, err := filepath.Abs(workspaceRoot())
	if err != nil {
		return "", "", "", err
	}

	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = "."
	}
	if !filepath.IsAbs(requested) {
		requested = filepath.Join(root, requested)
	}

	target, err := filepath.Abs(requested)
	if err != nil {
		return "", "", "", err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", "", fmt.Errorf("path escapes workspace: %s", requested)
	}
	if rel == "." {
		return root, target, ".", nil
	}
	return root, target, filepath.ToSlash(rel), nil
}

func shouldSkipName(name string) bool {
	switch name {
	case ".git", ".DS_Store":
		return true
	default:
		return false
	}
}

func clamp(value int, defaultValue int, maxValue int) int {
	if value <= 0 {
		return defaultValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
