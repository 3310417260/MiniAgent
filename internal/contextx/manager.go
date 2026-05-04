package contextx

import "miniagent/internal/llm"

type Manager interface {
	Build(messages []llm.Message) []llm.Message
}

type RecentNManager struct {
	MaxMessages int
}

func (m RecentNManager) Build(messages []llm.Message) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	if m.MaxMessages <= 0 {
		return append([]llm.Message(nil), messages...)
	}

	system, rest := splitSystemMessage(messages)
	if len(rest) > m.MaxMessages {
		rest = rest[len(rest)-m.MaxMessages:]
	}

	out := make([]llm.Message, 0, len(system)+len(rest))
	out = append(out, system...)
	out = append(out, rest...)
	return out
}

func splitSystemMessage(messages []llm.Message) ([]llm.Message, []llm.Message) {
	if messages[0].Role != llm.RoleSystem {
		return nil, append([]llm.Message(nil), messages...)
	}

	system := []llm.Message{messages[0]}
	rest := append([]llm.Message(nil), messages[1:]...)
	return system, rest
}
