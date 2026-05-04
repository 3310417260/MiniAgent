package prompt

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptAppendsProjectInstructions(t *testing.T) {
	got := BuildSystemPrompt("base", "Use Go idioms.")
	if !strings.Contains(got, "base") {
		t.Fatalf("prompt = %q, want base prompt", got)
	}
	if !strings.Contains(got, "# Project Instructions") {
		t.Fatalf("prompt = %q, want project instructions heading", got)
	}
	if !strings.Contains(got, "Use Go idioms.") {
		t.Fatalf("prompt = %q, want AGENTS.md content", got)
	}
}

func TestBuildSystemPromptWithoutProjectInstructions(t *testing.T) {
	got := BuildSystemPrompt("base", "   ")
	if got != "base" {
		t.Fatalf("prompt = %q, want base", got)
	}
}
