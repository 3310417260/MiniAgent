package logx

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Event struct {
	Time    time.Time      `json:"time"`
	Type    string         `json:"type"`
	Session string         `json:"session,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

type Logger interface {
	Log(ctx context.Context, event Event) error
}

type NoopLogger struct{}

func (NoopLogger) Log(ctx context.Context, event Event) error {
	return nil
}

type JSONLLogger struct {
	path string
	mu   sync.Mutex
}

func NewJSONLLogger(path string) *JSONLLogger {
	return &JSONLLogger{path: path}
}

func (l *JSONLLogger) Path() string {
	return l.path
}

func (l *JSONLLogger) Log(ctx context.Context, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if event.Time.IsZero() {
		event.Time = time.Now()
	}

	line, err := json.Marshal(event)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write(line)
	return err
}
