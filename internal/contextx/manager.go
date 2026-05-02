package contextx

import "miniagent/internal/llm"

type Manager interface {
	Build(messages []llm.Message) []llm.Message
}

type RecentNManager struct {
	MaxMessages int
}

func (m RecentNManager) Build(messages []llm.Message) []llm.Message {
	if m.MaxMessages <= 0 || len(messages) <= m.MaxMessages {
		return append([]llm.Message(nil), messages...)
	}

	start := len(messages) - m.MaxMessages
	return append([]llm.Message(nil), messages[start:]...)
}
