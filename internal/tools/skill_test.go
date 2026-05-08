package tools

import (
	"context"
	"encoding/json"
	"testing"

	"miniagent/internal/skill"
)

func TestSelectSkillToolSetsSelection(t *testing.T) {
	state := &skill.Selection{}
	tool := SelectSkillTool{
		State:        state,
		AllowedNames: []string{"pdf", "xlsx"},
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"name":"xlsx","reason":"spreadsheet task"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result = %+v, want success", result)
	}
	if state.Name != "xlsx" || state.Reason != "spreadsheet task" {
		t.Fatalf("selection = %+v, want xlsx", state)
	}
}

func TestSelectSkillToolRejectsUnknownSkill(t *testing.T) {
	state := &skill.Selection{}
	tool := SelectSkillTool{
		State:        state,
		AllowedNames: []string{"pdf"},
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"name":"docx","reason":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("result = %+v, want error", result)
	}
	if !state.Empty() {
		t.Fatalf("selection = %+v, want empty", state)
	}
}

func TestSelectSkillToolAllowsNone(t *testing.T) {
	state := &skill.Selection{}
	tool := SelectSkillTool{
		State:        state,
		AllowedNames: []string{"pdf"},
	}

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"name":"none","reason":"general chat"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result = %+v, want success", result)
	}
	if state.Name != skill.None {
		t.Fatalf("selection = %+v, want none", state)
	}
}
