package contextx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Summary struct {
	Content            string
	SummarizedMessages int
	UpdatedAt          time.Time
	Model              string
}

type SummaryStore interface {
	Load(ctx context.Context, sessionID string) (Summary, error)
	Save(ctx context.Context, sessionID string, summary Summary) error
	Clear(ctx context.Context, sessionID string) error
	Path(sessionID string) (string, error)
}

type FileSummaryStore struct {
	Dir string
}

func NewFileSummaryStore(dir string) *FileSummaryStore {
	return &FileSummaryStore{Dir: dir}
}

func (s *FileSummaryStore) Load(ctx context.Context, sessionID string) (Summary, error) {
	path, err := s.Path(sessionID)
	if err != nil {
		return Summary{}, err
	}
	if err := ctx.Err(); err != nil {
		return Summary{}, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Summary{}, nil
	}
	if err != nil {
		return Summary{}, fmt.Errorf("read summary: %w", err)
	}
	return parseSummaryMarkdown(string(data)), nil
}

func (s *FileSummaryStore) Save(ctx context.Context, sessionID string, summary Summary) error {
	path, err := s.Path(sessionID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("create summary dir: %w", err)
	}
	if summary.UpdatedAt.IsZero() {
		summary.UpdatedAt = time.Now()
	}

	data := formatSummaryMarkdown(sessionID, summary)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	return nil
}

func (s *FileSummaryStore) Clear(ctx context.Context, sessionID string) error {
	path, err := s.Path(sessionID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("clear summary: %w", err)
	}
	return nil
}

func (s *FileSummaryStore) Path(sessionID string) (string, error) {
	if !validSessionSummaryID(sessionID) {
		return "", fmt.Errorf("invalid session id %q: use letters, numbers, '.', '_' or '-'", sessionID)
	}
	return filepath.Join(s.Dir, sessionID+".md"), nil
}

func formatSummaryMarkdown(sessionID string, summary Summary) string {
	return fmt.Sprintf(`---
session: %s
summarized_messages: %d
updated_at: %s
summary_model: %s
---

%s
`, sessionID, summary.SummarizedMessages, summary.UpdatedAt.Format(time.RFC3339), summary.Model, strings.TrimSpace(summary.Content))
}

func parseSummaryMarkdown(data string) Summary {
	var summary Summary
	text := strings.TrimSpace(data)
	if !strings.HasPrefix(text, "---") {
		summary.Content = text
		return summary
	}

	rest := strings.TrimPrefix(text, "---")
	parts := strings.SplitN(rest, "---", 2)
	if len(parts) != 2 {
		summary.Content = text
		return summary
	}

	for _, line := range strings.Split(parts[0], "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "summarized_messages":
			if n, err := strconv.Atoi(value); err == nil {
				summary.SummarizedMessages = n
			}
		case "updated_at":
			if t, err := time.Parse(time.RFC3339, value); err == nil {
				summary.UpdatedAt = t
			}
		case "summary_model":
			summary.Model = value
		}
	}
	summary.Content = strings.TrimSpace(parts[1])
	return summary
}

func validSessionSummaryID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
