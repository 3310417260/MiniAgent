package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"miniagent/internal/llm"
)

const (
	defaultGrepMaxMatches = 50
	maxGrepMatches        = 200
	maxGrepFileBytes      = 1 << 20
)

type GrepTextTool struct{}

func (GrepTextTool) Name() string {
	return "grep_text"
}

func (GrepTextTool) Description() string {
	return "Search for literal text in workspace files."
}

func (GrepTextTool) Permission() Permission {
	return PermissionReadOnly
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
