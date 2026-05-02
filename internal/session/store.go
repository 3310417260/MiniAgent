package session

import (
	"context"

	"miniagent/internal/llm"
)

type Store interface {
	Append(ctx context.Context, sessionID string, msg llm.Message) error
	Load(ctx context.Context, sessionID string) ([]llm.Message, error)
}
