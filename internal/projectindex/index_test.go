package projectindex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanBuildsLightweightProjectMap(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/demo\n\ngo 1.22\n")
	writeFile(t, root, "cmd/demo/main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, root, "internal/app/app.go", "package app\n")
	writeFile(t, root, "README.md", "# Demo\n")
	writeFile(t, root, "docs/guide.md", "# Guide\n")
	writeFile(t, root, "skills/demo/SKILL.md", "---\nname: demo\ndescription: demo skill\n---\n")
	writeFile(t, root, ".git/ignored.go", "package ignored\n")

	idx, err := Scan(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}

	if idx.Module != "example.com/demo" {
		t.Fatalf("module = %q", idx.Module)
	}
	assertContains(t, idx.Entrypoints, "cmd/demo/main.go")
	assertContains(t, idx.Packages, "cmd/demo")
	assertContains(t, idx.Packages, "internal/app")
	assertContains(t, idx.Docs, "README.md")
	assertContains(t, idx.Docs, "docs/guide.md")
	assertContains(t, idx.Skills, "demo")
	assertNotContains(t, idx.Packages, ".git")

	rendered := idx.String()
	for _, want := range []string{"Workspace:", "Go module: example.com/demo", "Entrypoints:", "Packages:", "Skills:"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered index missing %q:\n%s", want, rendered)
		}
	}

	prompt := idx.Prompt()
	if !strings.Contains(prompt, "workspace map only") || !strings.Contains(prompt, "read_file") {
		t.Fatalf("prompt = %q, want map-only guidance", prompt)
	}
}

func writeFile(t *testing.T, root string, rel string, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("%q not found in %v", want, values)
}

func assertNotContains(t *testing.T, values []string, unwanted string) {
	t.Helper()
	for _, value := range values {
		if value == unwanted {
			t.Fatalf("%q unexpectedly found in %v", unwanted, values)
		}
	}
}
