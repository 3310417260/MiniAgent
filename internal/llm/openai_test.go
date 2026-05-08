package llm

import (
	"encoding/json"
	"testing"
)

func TestNewChatRequestIncludesEnableThinkingFalse(t *testing.T) {
	enabled := false
	client := &OpenAIClient{
		model:          "deepseek-v4-flash",
		enableThinking: &enabled,
	}

	data, err := json.Marshal(client.newChatRequest(GenerateRequest{
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	}, false))
	if err != nil {
		t.Fatal(err)
	}

	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	if value, ok := body["enable_thinking"].(bool); !ok || value {
		t.Fatalf("enable_thinking = %#v, want false in request body", body["enable_thinking"])
	}
}

func TestNewChatRequestOmitsEnableThinkingWhenUnset(t *testing.T) {
	client := &OpenAIClient{model: "glm-4.6v"}

	data, err := json.Marshal(client.newChatRequest(GenerateRequest{
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	}, false))
	if err != nil {
		t.Fatal(err)
	}

	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["enable_thinking"]; ok {
		t.Fatalf("enable_thinking should be omitted when unset: %s", string(data))
	}
}
