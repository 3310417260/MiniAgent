package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"miniagent/internal/llm"
)

const (
	defaultShellTimeoutSeconds = 30
	maxShellTimeoutSeconds     = 60
	defaultShellMaxOutputBytes = 12000
	maxShellOutputBytes        = 50000
)

type RunShellTool struct{}

func (RunShellTool) Name() string {
	return "run_shell"
}

func (RunShellTool) Description() string {
	return "Run a restricted allowlisted command in the workspace."
}

func (RunShellTool) Permission() Permission {
	return PermissionShell
}

func (RunShellTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{
		Name:        "run_shell",
		Description: "Run a restricted allowlisted command in the workspace. Requires explicit approval. Allowed examples: go version, go test ./..., git status --short, git diff --stat.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Executable name. Only allowlisted commands are accepted.",
				},
				"args": map[string]any{
					"type":        "array",
					"description": "Command arguments as separate array items. Do not pass a shell string.",
					"items": map[string]any{
						"type": "string",
					},
				},
				"timeout_seconds": map[string]any{
					"type":        "integer",
					"description": "Execution timeout in seconds. Defaults to 30 and is capped at 60.",
				},
				"max_output_bytes": map[string]any{
					"type":        "integer",
					"description": "Maximum stdout/stderr bytes to return. Defaults to 12000 and is capped at 50000.",
				},
			},
			"required": []string{"command", "args"},
		},
	}
}

func (RunShellTool) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	var args struct {
		Command        string   `json:"command"`
		Args           []string `json:"args"`
		TimeoutSeconds int      `json:"timeout_seconds"`
		MaxOutputBytes int      `json:"max_output_bytes"`
	}
	if err := json.Unmarshal(defaultJSON(input), &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}

	command := strings.TrimSpace(args.Command)
	if command == "" {
		return Result{Content: "command is required", IsError: true}, nil
	}
	if strings.ContainsAny(command, `/\`) {
		return Result{Content: "command must be an executable name, not a path", IsError: true}, nil
	}
	if !isAllowedShellCommand(command, args.Args) {
		return Result{Content: "command is not allowlisted: " + shellDisplay(command, args.Args), IsError: true}, nil
	}

	timeoutSeconds := clamp(args.TimeoutSeconds, defaultShellTimeoutSeconds, maxShellTimeoutSeconds)
	maxOutputBytes := clamp(args.MaxOutputBytes, defaultShellMaxOutputBytes, maxShellOutputBytes)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, command, args.Args...)
	cmd.Dir = workspaceRoot()

	stdout := &limitedBuffer{limit: maxOutputBytes}
	stderr := &limitedBuffer{limit: maxOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	exitCode := 0
	timedOut := false
	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			timedOut = true
			exitCode = -1
		} else {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				return Result{Content: "run command: " + err.Error(), IsError: true}, nil
			}
		}
	}

	lines := []string{
		"command: " + shellDisplay(command, args.Args),
		fmt.Sprintf("exit_code: %d", exitCode),
	}
	if timedOut {
		lines = append(lines, fmt.Sprintf("timed_out: true after %ds", timeoutSeconds))
	}
	lines = appendShellOutput(lines, "stdout", stdout)
	lines = appendShellOutput(lines, "stderr", stderr)

	return Result{
		Content: strings.Join(lines, "\n"),
		IsError: exitCode != 0,
	}, nil
}

type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		b.truncated = true
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string {
	return b.buf.String()
}

func appendShellOutput(lines []string, label string, output *limitedBuffer) []string {
	text := strings.TrimRight(output.String(), "\n")
	if text == "" {
		text = "<empty>"
	}
	lines = append(lines, label+":")
	lines = append(lines, text)
	if output.truncated {
		lines = append(lines, fmt.Sprintf("... %s truncated after %d bytes", label, output.limit))
	}
	return lines
}

func isAllowedShellCommand(command string, args []string) bool {
	switch command {
	case "go":
		return isAllowedGoCommand(args)
	case "git":
		return isAllowedGitCommand(args)
	default:
		return false
	}
}

func isAllowedGoCommand(args []string) bool {
	if len(args) == 1 && args[0] == "version" {
		return true
	}
	if len(args) < 2 || args[0] != "test" {
		return false
	}
	return isAllowedGoTestArgs(args[1:])
}

func isAllowedGoTestArgs(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case isAllowedGoPackage(arg):
			continue
		case arg == "-v" || arg == "-count=1":
			continue
		case arg == "-run":
			if i+1 >= len(args) || !isSafeRunPattern(args[i+1]) {
				return false
			}
			i++
		case strings.HasPrefix(arg, "-run="):
			if !isSafeRunPattern(strings.TrimPrefix(arg, "-run=")) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func isAllowedGoPackage(arg string) bool {
	if arg == "./..." {
		return true
	}
	if strings.Contains(arg, "..") || strings.ContainsAny(arg, " \t\n\r") {
		return false
	}
	return arg == "." ||
		strings.HasPrefix(arg, "./cmd/") ||
		strings.HasPrefix(arg, "./internal/")
}

func isSafeRunPattern(pattern string) bool {
	if pattern == "" || len(pattern) > 200 {
		return false
	}
	return !strings.ContainsAny(pattern, " \t\n\r;`$&<>")
}

func isAllowedGitCommand(args []string) bool {
	return equalStrings(args, []string{"status", "--short"}) ||
		equalStrings(args, []string{"diff", "--stat"})
}

func equalStrings(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func shellDisplay(command string, args []string) string {
	if len(args) == 0 {
		return command
	}
	return command + " " + strings.Join(args, " ")
}
