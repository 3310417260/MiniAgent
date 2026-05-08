package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"miniagent/internal/plan"
)

func TestReadFileToolReadsWorkspaceFile(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("README.md", []byte("hello MiniAgent"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := (ReadFileTool{}).Execute(context.Background(), json.RawMessage(`{"path":"README.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result is error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "hello MiniAgent") {
		t.Fatalf("content = %q, want file contents", result.Content)
	}
}

func TestReadFileToolRejectsPathOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	result, err := (ReadFileTool{}).Execute(context.Background(), json.RawMessage(`{"path":"../secret.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for path escape")
	}
}

func TestListFilesAndGrepText(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.Mkdir("internal", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("internal", "demo.go"), []byte("package demo\nfunc GenerateRequest() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	listResult, err := (ListFilesTool{}).Execute(context.Background(), json.RawMessage(`{"path":"."}`))
	if err != nil {
		t.Fatal(err)
	}
	if listResult.IsError || !strings.Contains(listResult.Content, "internal/") {
		t.Fatalf("list result = %+v, want internal directory", listResult)
	}
	if strings.Contains(listResult.Content, "internal/demo.go") {
		t.Fatalf("non-recursive list result = %+v, did not want nested file", listResult)
	}

	recursiveListResult, err := (ListFilesTool{}).Execute(context.Background(), json.RawMessage(`{"path":".","recursive":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if recursiveListResult.IsError || !strings.Contains(recursiveListResult.Content, "internal/demo.go") {
		t.Fatalf("recursive list result = %+v, want nested file", recursiveListResult)
	}

	grepResult, err := (GrepTextTool{}).Execute(context.Background(), json.RawMessage(`{"path":".","pattern":"GenerateRequest"}`))
	if err != nil {
		t.Fatal(err)
	}
	if grepResult.IsError || !strings.Contains(grepResult.Content, "internal/demo.go:2:func GenerateRequest() {}") {
		t.Fatalf("grep result = %+v, want match", grepResult)
	}

	caseSensitiveResult, err := (GrepTextTool{}).Execute(context.Background(), json.RawMessage(`{"path":".","pattern":"generaterequest"}`))
	if err != nil {
		t.Fatal(err)
	}
	if caseSensitiveResult.IsError || !strings.Contains(caseSensitiveResult.Content, "no matches") {
		t.Fatalf("case-sensitive grep result = %+v, want no matches", caseSensitiveResult)
	}

	ignoreCaseResult, err := (GrepTextTool{}).Execute(context.Background(), json.RawMessage(`{"path":".","pattern":"generaterequest","ignore_case":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if ignoreCaseResult.IsError || !strings.Contains(ignoreCaseResult.Content, "internal/demo.go:2:func GenerateRequest() {}") {
		t.Fatalf("ignore-case grep result = %+v, want match", ignoreCaseResult)
	}
}

func TestWriteFileToolWritesWorkspaceFile(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	result, err := (WriteFileTool{}).Execute(context.Background(), json.RawMessage(`{"path":"notes.txt","content":"hello write"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result is error: %s", result.Content)
	}

	data, err := os.ReadFile("notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello write" {
		t.Fatalf("file content = %q, want hello write", string(data))
	}
}

func TestWriteFileToolRequiresOverwriteForExistingFile(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("notes.txt", []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := (WriteFileTool{}).Execute(context.Background(), json.RawMessage(`{"path":"notes.txt","content":"new"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content, "already exists") {
		t.Fatalf("result = %+v, want overwrite refusal", result)
	}

	overwriteResult, err := (WriteFileTool{}).Execute(context.Background(), json.RawMessage(`{"path":"notes.txt","content":"new","overwrite":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if overwriteResult.IsError {
		t.Fatalf("overwrite result is error: %s", overwriteResult.Content)
	}
	data, err := os.ReadFile("notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("file content = %q, want new", string(data))
	}
}

func TestWriteFileToolRejectsPathOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	result, err := (WriteFileTool{}).Execute(context.Background(), json.RawMessage(`{"path":"../secret.txt","content":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for path escape")
	}
}

func TestEditFileToolReplacesUniqueText(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("notes.txt", []byte("hello MiniAgent\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := (EditFileTool{}).Execute(context.Background(), json.RawMessage(`{"path":"notes.txt","old_text":"hello MiniAgent","new_text":"hello edit_file"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result is error: %s", result.Content)
	}
	data, err := os.ReadFile("notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello edit_file\n" {
		t.Fatalf("file content = %q, want edited content", string(data))
	}
}

func TestEditFileToolRejectsMissingOldText(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("notes.txt", []byte("hello MiniAgent\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := (EditFileTool{}).Execute(context.Background(), json.RawMessage(`{"path":"notes.txt","old_text":"missing","new_text":"new"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content, "not found") {
		t.Fatalf("result = %+v, want missing old_text error", result)
	}
	assertToolErrorType(t, result, ErrorNotFound)
}

func TestEditFileToolRejectsAmbiguousOldText(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("notes.txt", []byte("target\ntarget\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := (EditFileTool{}).Execute(context.Background(), json.RawMessage(`{"path":"notes.txt","old_text":"target","new_text":"replacement"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content, "appears 2 times") {
		t.Fatalf("result = %+v, want ambiguous old_text error", result)
	}
	assertToolErrorType(t, result, ErrorNotUnique)
}

func TestEditFileToolRejectsPathOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	result, err := (EditFileTool{}).Execute(context.Background(), json.RawMessage(`{"path":"../secret.txt","old_text":"old","new_text":"new"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("result.IsError = false, want true for path escape")
	}
}

func TestRunShellToolRunsAllowedCommand(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	result, err := (RunShellTool{}).Execute(context.Background(), json.RawMessage(`{"command":"go","args":["version"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result is error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "command: go version") || !strings.Contains(result.Content, "go version") {
		t.Fatalf("content = %q, want go version output", result.Content)
	}
}

func TestRunShellToolRejectsNonAllowlistedCommand(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	result, err := (RunShellTool{}).Execute(context.Background(), json.RawMessage(`{"command":"rm","args":["-rf","."]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content, "not allowlisted") {
		t.Fatalf("result = %+v, want allowlist rejection", result)
	}
	assertToolErrorType(t, result, ErrorCommandNotAllowed)
}

func TestRunShellToolRejectsCommandPath(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	result, err := (RunShellTool{}).Execute(context.Background(), json.RawMessage(`{"command":"/bin/echo","args":["hello"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content, "not a path") {
		t.Fatalf("result = %+v, want command path rejection", result)
	}
}

func assertToolErrorType(t *testing.T, result Result, want ErrorType) {
	t.Helper()
	if result.Error == nil {
		t.Fatalf("result.Error = nil, want %s; content=%s", want, result.Content)
	}
	if result.Error.Type != want {
		t.Fatalf("error type = %s, want %s; content=%s", result.Error.Type, want, result.Content)
	}

	var payload struct {
		OK        bool      `json:"ok"`
		ErrorType ErrorType `json:"error_type"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("decode structured error: %v; content=%s", err, result.Content)
	}
	if payload.OK || payload.ErrorType != want {
		t.Fatalf("payload = %+v, want ok=false error_type=%s", payload, want)
	}
}

func TestRunShellToolAllowlist(t *testing.T) {
	allowed := []struct {
		command string
		args    []string
	}{
		{"go", []string{"version"}},
		{"go", []string{"test", "./..."}},
		{"go", []string{"test", "./internal/tools", "-run", "TestRunShellToolAllowlist"}},
		{"git", []string{"status", "--short"}},
		{"git", []string{"diff", "--stat"}},
	}
	for _, tc := range allowed {
		if !isAllowedShellCommand(tc.command, tc.args) {
			t.Fatalf("%s %v should be allowed", tc.command, tc.args)
		}
	}

	rejected := []struct {
		command string
		args    []string
	}{
		{"sh", []string{"-c", "go test ./..."}},
		{"go", []string{"env"}},
		{"go", []string{"test", "../..."}},
		{"go", []string{"test", "./...", "-exec", "echo"}},
		{"git", []string{"push"}},
		{"git", []string{"status"}},
	}
	for _, tc := range rejected {
		if isAllowedShellCommand(tc.command, tc.args) {
			t.Fatalf("%s %v should be rejected", tc.command, tc.args)
		}
	}
}

func TestSetPlanToolSetsPlanState(t *testing.T) {
	state := plan.NewState()
	result, err := (SetPlanTool{State: state}).Execute(context.Background(), json.RawMessage(`{"title":"Build feature","steps":["read code","write code"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result is error: %s", result.Content)
	}
	if state.Title != "Build feature" || len(state.Steps) != 2 {
		t.Fatalf("state = %+v, want title and two steps", state)
	}
}

func TestUpdatePlanToolUpdatesPlanState(t *testing.T) {
	state := plan.NewState()
	state.Set("Build feature", []string{"read code"})

	result, err := (UpdatePlanTool{State: state}).Execute(context.Background(), json.RawMessage(`{"index":1,"status":"done","text":"read existing code"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result is error: %s", result.Content)
	}
	if state.Steps[0].Status != plan.StatusDone || state.Steps[0].Text != "read existing code" {
		t.Fatalf("step = %+v, want updated done step", state.Steps[0])
	}
}
