package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const EnvVar = "MINIAGENT_WORKSPACE"

var (
	mu       sync.RWMutex
	override string
)

// Root returns the target project workspace. By default MiniAgent serves the
// current directory; MINIAGENT_WORKSPACE lets the harness point at another
// project without changing where sessions, logs, and local skills are stored.
func Root() (string, error) {
	configured := configuredRoot()
	if configured == "" {
		configured = "."
	}

	return resolve(configured)
}

// SetRoot changes the target workspace for the current MiniAgent process. This
// is used by the interactive /workspace command so learners can switch target
// projects without restarting the CLI.
func SetRoot(path string) (string, error) {
	root, err := resolve(path)
	if err != nil {
		return "", err
	}
	mu.Lock()
	override = root
	mu.Unlock()
	return root, nil
}

// ClearOverride removes the interactive override. The next Root call falls back
// to MINIAGENT_WORKSPACE or the current working directory.
func ClearOverride() {
	mu.Lock()
	override = ""
	mu.Unlock()
}

func configuredRoot() string {
	mu.RLock()
	current := override
	mu.RUnlock()
	if strings.TrimSpace(current) != "" {
		return current
	}
	return strings.TrimSpace(os.Getenv(EnvVar))
}

func resolve(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("workspace path is required")
	}

	root, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("stat workspace: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace is not a directory: %s", root)
	}
	return root, nil
}

// MustRoot is used in tool execution paths where the old behavior was to fall
// back to ".". If workspace resolution fails, keeping "." avoids panics while
// the caller still receives path/stat errors from the concrete operation.
func MustRoot() string {
	root, err := Root()
	if err != nil {
		return "."
	}
	return root
}
