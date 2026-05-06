package plan

import (
	"strings"
	"testing"
)

func TestStateSetAndString(t *testing.T) {
	state := NewState()
	state.Set("Build feature", []string{"read code", "", "write tests"})

	if len(state.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(state.Steps))
	}
	out := state.String()
	if !strings.Contains(out, "Plan: Build feature") || !strings.Contains(out, "1. [pending] read code") {
		t.Fatalf("plan output = %q, want title and pending step", out)
	}
}

func TestStateUpdate(t *testing.T) {
	state := NewState()
	state.Set("Build feature", []string{"read code"})

	if err := state.Update(1, StatusDone, "read existing code"); err != nil {
		t.Fatal(err)
	}
	if state.Steps[0].Status != StatusDone || state.Steps[0].Text != "read existing code" {
		t.Fatalf("step = %+v, want done with updated text", state.Steps[0])
	}
}

func TestStateRejectsInvalidUpdate(t *testing.T) {
	state := NewState()
	state.Set("Build feature", []string{"read code"})

	if err := state.Update(2, StatusDone, ""); err == nil {
		t.Fatal("expected out of range error")
	}
	if err := state.Update(1, StepStatus("weird"), ""); err == nil {
		t.Fatal("expected invalid status error")
	}
}
