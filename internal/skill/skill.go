package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Skill struct {
	Name        string
	Description string
	Path        string
	Content     string
}

type CatalogEntry struct {
	Name        string
	Description string
	Path        string
}

type Script struct {
	Path string
	Size int64
}

type Manifest struct {
	Scripts []ScriptPermission `json:"scripts"`
}

type ScriptPermission struct {
	Path             string          `json:"path"`
	Description      string          `json:"description"`
	Reason           string          `json:"reason"`
	Runner           string          `json:"runner"`
	Allowed          bool            `json:"allowed"`
	TimeoutSeconds   int             `json:"timeout_seconds"`
	MaxOutputBytes   int             `json:"max_output_bytes"`
	MaxArgs          int             `json:"max_args"`
	MaxArgBytes      int             `json:"max_arg_bytes"`
	RequiresApproval bool            `json:"requires_approval"`
	Args             []ArgPermission `json:"args"`
}

type ArgPermission struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type Store struct {
	Root string
}

func NewStore(root string) Store {
	return Store{Root: root}
}

func (s Store) List() ([]Skill, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var skills []Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skill, err := s.Load(entry.Name())
		if err != nil {
			continue
		}
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})
	return skills, nil
}

func (s Store) Catalog() ([]CatalogEntry, error) {
	skills, err := s.List()
	if err != nil {
		return nil, err
	}

	catalog := make([]CatalogEntry, 0, len(skills))
	for _, item := range skills {
		catalog = append(catalog, CatalogEntry{
			Name:        item.Name,
			Description: item.Description,
			Path:        item.Path,
		})
	}
	return catalog, nil
}

func (s Store) Load(name string) (Skill, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, "..") {
		return Skill{}, fmt.Errorf("invalid skill name: %q", name)
	}

	path := filepath.Join(s.Root, name, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}

	skill := parseSkill(string(data))
	if skill.Name == "" {
		skill.Name = name
	}
	skill.Path = path
	return skill, nil
}

func (s Store) Scripts(name string) ([]Script, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, "..") {
		return nil, fmt.Errorf("invalid skill name: %q", name)
	}

	scriptsDir := filepath.Join(s.Root, name, "scripts")
	entries := []Script{}
	if err := filepath.WalkDir(scriptsDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Join(s.Root, name), path)
		if err != nil {
			return err
		}
		entries = append(entries, Script{
			Path: filepath.ToSlash(rel),
			Size: info.Size(),
		})
		return nil
	}); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return entries, nil
}

func (s Store) Manifest(name string) (Manifest, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, "..") {
		return Manifest{}, "", fmt.Errorf("invalid skill name: %q", name)
	}

	path := filepath.Join(s.Root, name, "manifest.local.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{}, path, nil
		}
		return Manifest{}, path, err
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, path, fmt.Errorf("parse manifest.local.json: %w", err)
	}
	for i := range manifest.Scripts {
		manifest.Scripts[i].Path = filepath.ToSlash(strings.TrimSpace(manifest.Scripts[i].Path))
	}
	return manifest, path, nil
}

// parseSkill reads the tiny subset of SKILL.md metadata MiniAgent needs now.
// The markdown body stays intact because it is the part injected into prompts.
func parseSkill(raw string) Skill {
	var skill Skill
	content := strings.TrimSpace(raw)

	if strings.HasPrefix(content, "---\n") {
		rest := strings.TrimPrefix(content, "---\n")
		if end := strings.Index(rest, "\n---"); end >= 0 {
			meta := rest[:end]
			content = strings.TrimSpace(rest[end+len("\n---"):])
			for _, line := range strings.Split(meta, "\n") {
				key, value, ok := strings.Cut(line, ":")
				if !ok {
					continue
				}
				value = strings.Trim(strings.TrimSpace(value), `"'`)
				switch strings.TrimSpace(key) {
				case "name":
					skill.Name = value
				case "description":
					skill.Description = value
				}
			}
		}
	}

	skill.Content = content
	return skill
}

// Prompt is the text that can be placed into a model system/developer prompt.
// Loading a skill does not execute scripts; it only exposes instructions.
func (s Skill) Prompt() string {
	return strings.TrimSpace(fmt.Sprintf("Skill: %s\n\n%s", s.Name, s.Content))
}
