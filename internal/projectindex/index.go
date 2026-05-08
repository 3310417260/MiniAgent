package projectindex

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultMaxPackages = 40
	defaultMaxDocs     = 30
	defaultMaxSkills   = 30
)

type Options struct {
	MaxPackages int
	MaxDocs     int
	MaxSkills   int
}

type Index struct {
	Workspace   string
	Module      string
	Entrypoints []string
	Packages    []string
	Docs        []string
	Skills      []string
	ConfigFiles []string
}

func Scan(ctx context.Context, root string, opts Options) (Index, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Index{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return Index{}, fmt.Errorf("stat workspace: %w", err)
	}
	if !info.IsDir() {
		return Index{}, fmt.Errorf("workspace is not a directory: %s", root)
	}

	opts = normalizeOptions(opts)
	index := Index{
		Workspace: root,
		Module:    readGoModuleName(filepath.Join(root, "go.mod")),
	}

	packages := map[string]bool{}
	entrypoints := map[string]bool{}
	docs := map[string]bool{}
	configs := map[string]bool{}

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipFile(entry.Name()) {
			return nil
		}

		rel := slashRel(root, path)
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))

		switch {
		case ext == ".go" && !strings.HasSuffix(name, "_test.go"):
			dir := filepath.ToSlash(filepath.Dir(rel))
			if dir == "." {
				dir = "."
			}
			packages[dir] = true
			if isMainGoFile(path) {
				entrypoints[rel] = true
			}
		case ext == ".md":
			docs[rel] = true
		case isConfigName(name):
			configs[rel] = true
		}
		return nil
	})
	if err != nil {
		return Index{}, err
	}

	index.Packages = sortedKeys(packages, opts.MaxPackages)
	index.Entrypoints = sortedKeys(entrypoints, 0)
	index.Docs = sortedKeys(docs, opts.MaxDocs)
	index.ConfigFiles = sortedKeys(configs, 0)
	index.Skills = scanSkills(ctx, filepath.Join(root, "skills"), opts.MaxSkills)
	return index, nil
}

func (idx Index) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Workspace: %s\n", idx.Workspace)
	if idx.Module != "" {
		fmt.Fprintf(&b, "Go module: %s\n", idx.Module)
	} else {
		b.WriteString("Go module: <not found>\n")
	}

	writeSection(&b, "Entrypoints", idx.Entrypoints)
	writeSection(&b, "Packages", idx.Packages)
	writeSection(&b, "Docs", idx.Docs)
	writeSection(&b, "Skills", idx.Skills)
	writeSection(&b, "Config files", idx.ConfigFiles)
	return strings.TrimRight(b.String(), "\n")
}

func (idx Index) Prompt() string {
	return "Project context (workspace map only; use tools such as read_file or grep_text to inspect file contents):\n" + idx.String()
}

func normalizeOptions(opts Options) Options {
	if opts.MaxPackages <= 0 {
		opts.MaxPackages = defaultMaxPackages
	}
	if opts.MaxDocs <= 0 {
		opts.MaxDocs = defaultMaxDocs
	}
	if opts.MaxSkills <= 0 {
		opts.MaxSkills = defaultMaxSkills
	}
	return opts
}

func readGoModuleName(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func isMainGoFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	text := string(data)
	return strings.Contains(text, "package main") && strings.Contains(text, "func main()")
}

func scanSkills(ctx context.Context, skillsRoot string, max int) []string {
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		return nil
	}

	var skills []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return skills
		}
		if !entry.IsDir() || shouldSkipDir(entry.Name()) {
			continue
		}
		if _, err := os.Stat(filepath.Join(skillsRoot, entry.Name(), "SKILL.md")); err == nil {
			skills = append(skills, entry.Name())
		}
	}
	sort.Strings(skills)
	if max > 0 && len(skills) > max {
		return append(skills[:max:max], fmt.Sprintf("... truncated after %d skills", max))
	}
	return skills
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".idea", ".vscode", "node_modules", "vendor", "sessions", "logs":
		return true
	default:
		return false
	}
}

func shouldSkipFile(name string) bool {
	return name == ".DS_Store"
}

func isConfigName(name string) bool {
	switch name {
	case "go.mod", "go.sum", "AGENTS.md", "README.md", ".gitignore", "manifest.local.json":
		return true
	default:
		return false
	}
}

func sortedKeys(values map[string]bool, max int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if max > 0 && len(keys) > max {
		return append(keys[:max:max], fmt.Sprintf("... truncated after %d entries", max))
	}
	return keys
}

func slashRel(root string, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func writeSection(b *strings.Builder, title string, values []string) {
	fmt.Fprintf(b, "\n%s:\n", title)
	if len(values) == 0 {
		b.WriteString("- <none>\n")
		return
	}
	for _, value := range values {
		fmt.Fprintf(b, "- %s\n", value)
	}
}
