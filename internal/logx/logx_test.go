package logx

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestJSONLLoggerWritesEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "miniagent.jsonl")
	logger := NewJSONLLogger(path)

	if err := logger.Log(context.Background(), Event{
		Type:    "tool_result",
		Session: "default",
		Data: map[string]any{
			"tool":     "read_file",
			"is_error": false,
		},
	}); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("expected one log line")
	}
	var event Event
	if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "tool_result" || event.Session != "default" {
		t.Fatalf("event = %+v, want tool_result/default", event)
	}
	if event.Data["tool"] != "read_file" {
		t.Fatalf("data = %+v, want tool=read_file", event.Data)
	}
}
