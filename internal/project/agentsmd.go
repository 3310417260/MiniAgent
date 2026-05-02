package project

import (
	"errors"
	"os"
	"path/filepath"
)

func FindAGENTSMD(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	for {
		candidate := filepath.Join(dir, "AGENTS.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("AGENTS.md not found")
		}
		dir = parent
	}
}
