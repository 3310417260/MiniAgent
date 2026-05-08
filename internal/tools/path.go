package tools

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"miniagent/internal/workspace"
)

func defaultJSON(input json.RawMessage) json.RawMessage {
	if len(strings.TrimSpace(string(input))) == 0 {
		return json.RawMessage(`{}`)
	}
	return input
}

func workspaceRoot() string {
	return workspace.MustRoot()
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
