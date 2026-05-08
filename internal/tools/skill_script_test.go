package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSkillScriptToolRunsAllowlistedDemoScript(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeDemoScript(t, root, "printf 'demo script\\n'; printf 'args:%s:%s\\n' \"$1\" \"$2\"")
	writeDemoManifest(t, root, 5, 4000, 12, 200)

	result, err := (RunSkillScriptTool{}).Execute(context.Background(), json.RawMessage(`{"skill":"demo","script":"scripts/echo_args.sh","args":["hello","skill"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result is error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "skill_script: skills/demo/scripts/echo_args.sh") ||
		!strings.Contains(result.Content, "args: hello skill") ||
		!strings.Contains(result.Content, "demo script") ||
		!strings.Contains(result.Content, "args:hello:skill") {
		t.Fatalf("content = %q, want script output", result.Content)
	}
	if len(result.Events) != 1 || result.Events[0].Type != "skill_script_execute" {
		t.Fatalf("events = %+v, want skill_script_execute", result.Events)
	}
	if result.Events[0].Data["exit_code"] != 0 || result.Events[0].Data["timed_out"] != false {
		t.Fatalf("event data = %+v, want exit_code=0 timed_out=false", result.Events[0].Data)
	}
}

func TestRunSkillScriptToolRunsPython3Runner(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeDemoPythonScript(t, root)
	writeDemoManifestWithPython(t, root)

	result, err := (RunSkillScriptTool{}).Execute(context.Background(), json.RawMessage(`{"skill":"demo","script":"scripts/inspect_args.py","args":["sample","7"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result is error: %s", result.Content)
	}
	if !strings.Contains(result.Content, `"label": "sample"`) || !strings.Contains(result.Content, `"count": 7`) {
		t.Fatalf("content = %q, want python JSON output", result.Content)
	}
	if len(result.Events) != 1 || result.Events[0].Data["runner"] != "python3" {
		t.Fatalf("events = %+v, want runner python3", result.Events)
	}
}

func TestRunSkillScriptToolValidatesManifestArgTypes(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeDemoPythonScript(t, root)
	writeDemoManifestWithPython(t, root)

	result, err := (RunSkillScriptTool{}).Preflight(context.Background(), json.RawMessage(`{"skill":"demo","script":"scripts/inspect_args.py","args":["sample","not-an-int"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content, "must be an integer") {
		t.Fatalf("result = %+v, want integer validation error", result)
	}
}

func TestRunSkillScriptToolRejectsNonAllowlistedScript(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeDemoScript(t, root, "printf 'demo\\n'")
	writeDemoManifest(t, root, 5, 4000, 12, 200)

	result, err := (RunSkillScriptTool{}).Execute(context.Background(), json.RawMessage(`{"skill":"xlsx","script":"scripts/recalc.py","args":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content, "not allowlisted") {
		t.Fatalf("result = %+v, want allowlist rejection", result)
	}
	if result.Error == nil || result.Error.Type != ErrorScriptNotAllowed {
		t.Fatalf("tool error = %+v, want script_not_allowed", result.Error)
	}
	if len(result.Events) != 1 || result.Events[0].Type != "skill_script_preflight" {
		t.Fatalf("events = %+v, want skill_script_preflight", result.Events)
	}
	if result.Events[0].Data["allowed"] != false || result.Events[0].Data["reason"] != "not_allowlisted" {
		t.Fatalf("event data = %+v, want not allowed", result.Events[0].Data)
	}
}

func TestRunSkillScriptToolPreflightRejectsNonAllowlistedScript(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeDemoScript(t, root, "printf 'demo\\n'")
	writeDemoManifest(t, root, 5, 4000, 12, 200)

	result, err := (RunSkillScriptTool{}).Preflight(context.Background(), json.RawMessage(`{"skill":"xlsx","script":"scripts/recalc.py","args":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content, "not allowlisted") {
		t.Fatalf("result = %+v, want allowlist rejection", result)
	}
	if result.Error == nil || result.Error.Type != ErrorScriptNotAllowed {
		t.Fatalf("tool error = %+v, want script_not_allowed", result.Error)
	}
	if len(result.Events) != 1 || result.Events[0].Data["allowed"] != false {
		t.Fatalf("events = %+v, want disallowed preflight event", result.Events)
	}
}

func TestRunSkillScriptToolPreflightAllowsManifestScript(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeDemoScript(t, root, "printf 'demo\\n'")
	writeDemoManifest(t, root, 5, 4000, 12, 200)

	result, err := (RunSkillScriptTool{}).Preflight(context.Background(), json.RawMessage(`{"skill":"demo","script":"scripts/echo_args.sh","args":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result = %+v, want success", result)
	}
	if len(result.Events) != 1 || result.Events[0].Type != "skill_script_preflight" {
		t.Fatalf("events = %+v, want skill_script_preflight", result.Events)
	}
	if result.Events[0].Data["allowed"] != true {
		t.Fatalf("event data = %+v, want allowed=true", result.Events[0].Data)
	}
}

func TestRunSkillScriptToolRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeDemoManifest(t, root, 5, 4000, 12, 200)

	result, err := (RunSkillScriptTool{}).Execute(context.Background(), json.RawMessage(`{"skill":"demo","script":"scripts/../echo_args.sh","args":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("result = %+v, want path rejection", result)
	}
}

func TestRunSkillScriptToolTruncatesOutput(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeDemoScript(t, root, "printf '1234567890abcdef\\n'")
	writeDemoManifest(t, root, 5, 4, 12, 200)

	result, err := (RunSkillScriptTool{}).Execute(context.Background(), json.RawMessage(`{"skill":"demo","script":"scripts/echo_args.sh","args":[],"max_output_bytes":4}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result is error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "1234") || !strings.Contains(result.Content, "stdout truncated after 4 bytes") {
		t.Fatalf("content = %q, want truncated output", result.Content)
	}
	if len(result.Events) != 1 || result.Events[0].Data["stdout_truncated"] != true {
		t.Fatalf("events = %+v, want stdout_truncated", result.Events)
	}
}

func TestRunSkillScriptToolTimesOut(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeDemoScript(t, root, "sleep 2")
	writeDemoManifest(t, root, 1, 4000, 12, 200)

	result, err := (RunSkillScriptTool{}).Execute(context.Background(), json.RawMessage(`{"skill":"demo","script":"scripts/echo_args.sh","args":[],"timeout_seconds":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content, "timed_out: true") {
		t.Fatalf("result = %+v, want timeout error", result)
	}
	if len(result.Events) != 1 || result.Events[0].Data["timed_out"] != true {
		t.Fatalf("events = %+v, want timed_out", result.Events)
	}
}

func writeDemoScript(t *testing.T, root string, body string) {
	t.Helper()
	path := filepath.Join(root, "skills", "demo", "scripts", "echo_args.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeDemoPythonScript(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "skills", "demo", "scripts", "inspect_args.py")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `#!/usr/bin/env python3
import json
import sys
print(json.dumps({"label": sys.argv[1], "count": int(sys.argv[2])}, indent=2))
`
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeDemoManifest(t *testing.T, root string, timeoutSeconds int, maxOutputBytes int, maxArgs int, maxArgBytes int) {
	t.Helper()
	path := filepath.Join(root, "skills", "demo", "manifest.local.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf(`{
  "scripts": [
    {
      "path": "scripts/echo_args.sh",
      "description": "Safe demo script.",
      "allowed": true,
      "requires_approval": true,
      "timeout_seconds": %d,
      "max_output_bytes": %d,
      "max_args": %d,
      "max_arg_bytes": %d
    }
  ]
}
`, timeoutSeconds, maxOutputBytes, maxArgs, maxArgBytes)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeDemoManifestWithPython(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "skills", "demo", "manifest.local.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{
  "scripts": [
    {
      "path": "scripts/inspect_args.py",
      "description": "Safe Python demo script.",
      "runner": "python3",
      "allowed": true,
      "requires_approval": true,
      "timeout_seconds": 5,
      "max_output_bytes": 4000,
      "max_args": 2,
      "max_arg_bytes": 100,
      "args": [
        {"type": "string", "description": "Label."},
        {"type": "integer", "description": "Count."}
      ]
    }
  ]
}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
