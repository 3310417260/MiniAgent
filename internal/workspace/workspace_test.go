package workspace

import (
	"path/filepath"
	"testing"
)

func TestRootDefaultsToCurrentDirectory(t *testing.T) {
	ClearOverride()
	t.Setenv(EnvVar, "")

	got, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Root() = %q, want %q", got, want)
	}
}

func TestSetRootOverridesEnv(t *testing.T) {
	ClearOverride()
	defer ClearOverride()

	envRoot := t.TempDir()
	overrideRoot := t.TempDir()
	t.Setenv(EnvVar, envRoot)

	got, err := SetRoot(overrideRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got != overrideRoot {
		t.Fatalf("SetRoot() = %q, want %q", got, overrideRoot)
	}

	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if root != overrideRoot {
		t.Fatalf("Root() = %q, want override %q", root, overrideRoot)
	}
}

func TestClearOverrideFallsBackToEnv(t *testing.T) {
	ClearOverride()

	envRoot := t.TempDir()
	overrideRoot := t.TempDir()
	t.Setenv(EnvVar, envRoot)

	if _, err := SetRoot(overrideRoot); err != nil {
		t.Fatal(err)
	}
	ClearOverride()

	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if root != envRoot {
		t.Fatalf("Root() = %q, want env %q", root, envRoot)
	}
}
