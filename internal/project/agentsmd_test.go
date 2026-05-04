package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindAGENTSMDSearchesParents(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	agentsPath := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte("instructions"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := FindAGENTSMD(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != agentsPath {
		t.Fatalf("path = %q, want %q", got, agentsPath)
	}
}
