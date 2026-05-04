package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
