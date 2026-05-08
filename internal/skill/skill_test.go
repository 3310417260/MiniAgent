package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreLoadParsesSkill(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "planner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "planner", "SKILL.md"), []byte(`---
name: planner
description: Use for planning.
---

# Planner

Plan carefully.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewStore(root)
	skill, err := store.Load("planner")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Name != "planner" || skill.Description != "Use for planning." {
		t.Fatalf("skill = %+v, want parsed metadata", skill)
	}
	if !strings.Contains(skill.Content, "Plan carefully.") {
		t.Fatalf("content = %q, want markdown body", skill.Content)
	}
}

func TestStoreListSortsSkills(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"xlsx", "planner"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, "SKILL.md"), []byte("---\nname: "+name+"\n---\n\n# "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	skills, err := NewStore(root).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 || skills[0].Name != "planner" || skills[1].Name != "xlsx" {
		t.Fatalf("skills = %+v, want sorted planner/xlsx", skills)
	}
}

func TestStoreCatalogOmitsSkillBody(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pdf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pdf", "SKILL.md"), []byte(`---
name: pdf
description: Work with PDF files.
---

# PDF

Long private instructions should not be part of the catalog.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	catalog, err := NewStore(root).Catalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 1 {
		t.Fatalf("catalog = %+v, want one entry", catalog)
	}
	if catalog[0].Name != "pdf" || catalog[0].Description != "Work with PDF files." {
		t.Fatalf("catalog = %+v, want metadata only", catalog)
	}
}

func TestStoreScriptsListsScriptsSorted(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		filepath.Join(root, "demo", "scripts", "z.sh"),
		filepath.Join(root, "demo", "scripts", "nested", "a.sh"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("echo demo\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	scripts, err := NewStore(root).Scripts("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 2 {
		t.Fatalf("scripts = %+v, want two entries", scripts)
	}
	if scripts[0].Path != "scripts/nested/a.sh" || scripts[1].Path != "scripts/z.sh" {
		t.Fatalf("scripts = %+v, want sorted slash paths", scripts)
	}
}

func TestStoreScriptsMissingDirectoryReturnsEmpty(t *testing.T) {
	scripts, err := NewStore(t.TempDir()).Scripts("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 0 {
		t.Fatalf("scripts = %+v, want empty", scripts)
	}
}

func TestStoreManifestLoadsLocalPermissions(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "demo", "manifest.local.json"), []byte(`{
  "scripts": [
    {
      "path": "scripts/echo_args.sh",
      "description": "Safe demo script.",
      "reason": "Reviewed.",
      "allowed": true,
      "requires_approval": true,
      "timeout_seconds": 5,
      "max_output_bytes": 4000
    }
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest, path, err := NewStore(root).Manifest("demo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, filepath.Join("demo", "manifest.local.json")) {
		t.Fatalf("path = %q, want manifest path", path)
	}
	if len(manifest.Scripts) != 1 || manifest.Scripts[0].Path != "scripts/echo_args.sh" || !manifest.Scripts[0].Allowed || manifest.Scripts[0].Reason != "Reviewed." {
		t.Fatalf("manifest = %+v, want allowed script", manifest)
	}
}

func TestStoreManifestMissingReturnsEmpty(t *testing.T) {
	manifest, _, err := NewStore(t.TempDir()).Manifest("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Scripts) != 0 {
		t.Fatalf("manifest = %+v, want empty", manifest)
	}
}

func TestStoreRejectsInvalidSkillName(t *testing.T) {
	_, err := NewStore(t.TempDir()).Load("../secret")
	if err == nil {
		t.Fatal("expected invalid skill name error")
	}
}
