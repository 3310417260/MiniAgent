package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"miniagent/internal/llm"
	"miniagent/internal/skill"
)

type SelectSkillTool struct {
	State        *skill.Selection
	AllowedNames []string
}

func (SelectSkillTool) Name() string {
	return "select_skill"
}

func (SelectSkillTool) Description() string {
	return "Select the most relevant local skill for a user task."
}

func (SelectSkillTool) Permission() Permission {
	return PermissionAgentState
}

func (t SelectSkillTool) Schema() llm.ToolSchema {
	allowed := append([]string(nil), t.AllowedNames...)
	allowed = append(allowed, skill.None)

	return llm.ToolSchema{
		Name:        "select_skill",
		Description: "Select at most one skill from the provided catalog. Use name \"none\" when no skill applies.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "Selected skill name from the catalog, or \"none\".",
					"enum":        allowed,
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Short reason for the selection.",
				},
			},
			"required": []string{"name", "reason"},
		},
	}
}

func (t SelectSkillTool) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	if t.State == nil {
		return Result{Content: "skill selection state is not configured", IsError: true}, nil
	}

	var args struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(defaultJSON(input), &args); err != nil {
		return Result{Content: "invalid arguments: " + err.Error(), IsError: true}, nil
	}

	name := strings.TrimSpace(args.Name)
	if name == "" {
		return Result{Content: "name is required", IsError: true}, nil
	}
	if !t.allowed(name) {
		return Result{Content: fmt.Sprintf("skill %q is not in the catalog", name), IsError: true}, nil
	}

	t.State.Set(name, args.Reason)
	return Result{Content: "skill selected:\n" + t.State.String()}, nil
}

func (t SelectSkillTool) allowed(name string) bool {
	if name == skill.None {
		return true
	}
	for _, allowed := range t.AllowedNames {
		if name == allowed {
			return true
		}
	}
	return false
}
