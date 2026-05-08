package contextx

import (
	"context"
	"strings"

	"miniagent/internal/llm"
)

type ReadOnlySummaryManager struct {
	Store          SummaryStore
	RecentMessages int
}

func (m ReadOnlySummaryManager) Build(ctx context.Context, messages []llm.Message, opts BuildOptions) ([]llm.Message, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	if m.Store == nil || opts.SessionID == "" {
		return RecentNManager{MaxMessages: m.recentMessages()}.Build(ctx, messages, opts)
	}

	system, rest := splitSystemMessage(messages)
	summary, err := m.Store.Load(ctx, opts.SessionID)
	if err != nil {
		return nil, err
	}

	recent := rest
	if max := m.recentMessages(); max > 0 && len(recent) > max {
		recent = recent[len(recent)-max:]
	}

	out := make([]llm.Message, 0, len(system)+1+len(recent))
	out = append(out, system...)
	if strings.TrimSpace(summary.Content) != "" {
		out = append(out, llm.Message{
			Role:    llm.RoleSystem,
			Content: summarySystemMessage(summary.Content),
		})
	}
	out = append(out, recent...)
	return out, nil
}

func (m ReadOnlySummaryManager) recentMessages() int {
	if m.RecentMessages <= 0 {
		return defaultSummaryRecentMessages
	}
	return m.RecentMessages
}
