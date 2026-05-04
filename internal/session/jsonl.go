package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"miniagent/internal/llm"
)

type Entry struct {
	Timestamp time.Time   `json:"timestamp"`
	Message   llm.Message `json:"message"`
}

type JSONLStore struct {
	Dir string
}

func NewJSONLStore(dir string) *JSONLStore {
	return &JSONLStore{Dir: dir}
}

func (s *JSONLStore) Append(ctx context.Context, sessionID string, msg llm.Message) error {
	path, err := s.path(sessionID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open session file: %w", err)
	}
	defer file.Close()

	entry := Entry{
		Timestamp: time.Now(),
		Message:   msg,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode session entry: %w", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write session entry: %w", err)
	}
	return nil
}

func (s *JSONLStore) Load(ctx context.Context, sessionID string) ([]llm.Message, error) {
	path, err := s.path(sessionID)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open session file: %w", err)
	}
	defer file.Close()

	var messages []llm.Message
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("decode %s line %d: %w", filepath.Base(path), lineNo, err)
		}
		messages = append(messages, entry.Message)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read session file: %w", err)
	}
	return messages, nil
}

func (s *JSONLStore) List(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session dir: %w", err)
	}

	var ids []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		ids = append(ids, strings.TrimSuffix(entry.Name(), ".jsonl"))
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *JSONLStore) Clear(ctx context.Context, sessionID string) error {
	path, err := s.path(sessionID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("clear session file: %w", err)
	}
	return file.Close()
}

func (s *JSONLStore) path(sessionID string) (string, error) {
	if !ValidID(sessionID) {
		return "", fmt.Errorf("invalid session id %q: use letters, numbers, '.', '_' or '-'", sessionID)
	}
	return filepath.Join(s.Dir, sessionID+".jsonl"), nil
}

func ValidID(id string) bool {
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
