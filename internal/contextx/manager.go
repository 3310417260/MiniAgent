package contextx

import (
	"context"

	"miniagent/internal/llm"
)

type BuildOptions struct {
	SessionID string
	DebugAPI  bool
}

type Manager interface {
	Build(ctx context.Context, messages []llm.Message, opts BuildOptions) ([]llm.Message, error)
}

type RecentNManager struct {
	MaxMessages int
}

func (m RecentNManager) Build(ctx context.Context, messages []llm.Message, opts BuildOptions) ([]llm.Message, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	if m.MaxMessages <= 0 {
		return append([]llm.Message(nil), messages...), nil
	}

	system, rest := splitSystemMessage(messages)
	if len(rest) > m.MaxMessages {
		rest = rest[len(rest)-m.MaxMessages:]
	}

	out := make([]llm.Message, 0, len(system)+len(rest))
	out = append(out, system...)
	out = append(out, rest...)
	return out, nil
}

func splitSystemMessage(messages []llm.Message) ([]llm.Message, []llm.Message) {
	if messages[0].Role != llm.RoleSystem {
		return nil, append([]llm.Message(nil), messages...)
	}

	system := []llm.Message{messages[0]}
	rest := append([]llm.Message(nil), messages[1:]...)
	return system, rest
}
