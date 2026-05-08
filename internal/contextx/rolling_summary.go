package contextx

import (
	"context"
	"fmt"
	"strings"
	"time"

	"miniagent/internal/llm"
	"miniagent/internal/logx"
)

const (
	defaultSummaryTriggerMessages = 40
	defaultSummaryKeepMessages    = 20
	defaultSummaryBatchMessages   = 5
	defaultSummaryMaxChars        = 3000
	defaultSummaryRecentMessages  = 20
	defaultSummaryTimeoutSeconds  = 20
)

type RollingSummaryManager struct {
	Client           llm.Client
	Store            SummaryStore
	Logger           logx.Logger
	RecentMessages   int
	TriggerMessages  int
	KeepMessages     int
	BatchMessages    int
	MaxSummaryChars  int
	TimeoutSeconds   int
	SummaryModelName string
}

func (m RollingSummaryManager) Build(ctx context.Context, messages []llm.Message, opts BuildOptions) ([]llm.Message, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	if m.Store == nil || m.Client == nil || opts.SessionID == "" {
		return RecentNManager{MaxMessages: m.recentMessages()}.Build(ctx, messages, opts)
	}

	system, rest := splitSystemMessage(messages)
	summary, err := m.Store.Load(ctx, opts.SessionID)
	if err != nil {
		return nil, err
	}

	summary, updated, compressed, err := m.refreshSummaryIfNeeded(ctx, opts, summary, rest)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		fmt.Printf("Context summary: update failed, using previous summary and recent messages: %v\n", err)
		m.log(ctx, opts.SessionID, "context_summary_failed", map[string]any{
			"error":               err.Error(),
			"message_count":       len(rest),
			"kept_messages":       m.keepMessages(),
			"summary_model":       m.summaryModelName(),
			"summarized_messages": summary.SummarizedMessages,
		})
	}
	if updated {
		fmt.Printf("Context summary: compressed %d messages, kept %d recent messages, summary %d chars\n", compressed, m.keepMessages(), len([]rune(summary.Content)))
		m.log(ctx, opts.SessionID, "context_summary_updated", map[string]any{
			"compressed_messages": compressed,
			"kept_messages":       m.keepMessages(),
			"summary_chars":       len([]rune(summary.Content)),
			"summary_model":       m.summaryModelName(),
			"summarized_messages": summary.SummarizedMessages,
		})
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

func (m RollingSummaryManager) refreshSummaryIfNeeded(ctx context.Context, opts BuildOptions, summary Summary, rest []llm.Message) (Summary, bool, int, error) {
	if len(rest) <= m.triggerMessages() {
		return summary, false, 0, nil
	}

	if summary.SummarizedMessages < 0 || summary.SummarizedMessages > len(rest) {
		summary.SummarizedMessages = 0
	}

	target := len(rest) - m.keepMessages()
	if target <= summary.SummarizedMessages {
		return summary, false, 0, nil
	}

	toCompress := rest[summary.SummarizedMessages:target]
	if len(toCompress) < m.batchMessages() {
		return summary, false, 0, nil
	}
	content, err := m.generateSummary(ctx, summary.Content, toCompress, opts.DebugAPI)
	if err != nil {
		return Summary{}, false, 0, err
	}

	summary.Content = clampRunes(strings.TrimSpace(content), m.maxSummaryChars())
	summary.SummarizedMessages = target
	summary.UpdatedAt = time.Now()
	summary.Model = m.summaryModelName()
	if err := m.Store.Save(ctx, opts.SessionID, summary); err != nil {
		return Summary{}, false, 0, err
	}
	return summary, true, len(toCompress), nil
}

func (m RollingSummaryManager) generateSummary(ctx context.Context, previous string, messages []llm.Message, debugAPI bool) (string, error) {
	summaryCtx, cancel := context.WithTimeout(ctx, time.Duration(m.timeoutSeconds())*time.Second)
	defer cancel()

	resp, err := m.Client.Generate(summaryCtx, llm.GenerateRequest{
		DebugAPI: debugAPI,
		Messages: []llm.Message{
			{
				Role:    llm.RoleSystem,
				Content: summaryPrompt(m.maxSummaryChars()),
			},
			{
				Role: llm.RoleUser,
				Content: fmt.Sprintf("Previous summary:\n%s\n\nNew older messages to merge:\n%s",
					emptyAsNone(previous),
					formatMessagesForSummary(messages),
				),
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("generate rolling summary: %w", err)
	}
	return resp.Assistant.Content, nil
}

func summaryPrompt(maxChars int) string {
	return fmt.Sprintf(`You update MiniAgent's rolling conversation summary.

Return Markdown only. Keep the summary under %d characters.

Preserve stable, useful facts for future agent work:
- user profile, preferences, and learning goals
- project state, branch, implemented capabilities, and important files
- completed work
- current focus
- decisions already made
- open questions or pending tasks
- important commands, paths, environment variables, limits, and permissions

Rules:
- Merge the previous summary with the new older messages.
- If previous summary conflicts with new messages, prefer the new messages.
- Do not invent facts.
- Do not keep ordinary greetings or filler.
- Recent raw messages are more authoritative than this summary.

Use this exact section structure:

# Conversation Summary

## User Profile

## Project State

## Completed Work

## Current Focus

## Decisions

## Open Questions

## Important Details
`, maxChars)
}

func summarySystemMessage(content string) string {
	return "The following is a compressed rolling summary of earlier conversation history. Use it as background context. Recent raw messages after this summary are more authoritative.\n\n" + strings.TrimSpace(content)
}

func formatMessagesForSummary(messages []llm.Message) string {
	var b strings.Builder
	for i, msg := range messages {
		fmt.Fprintf(&b, "%02d. role=%s", i+1, msg.Role)
		if msg.ToolName != "" {
			fmt.Fprintf(&b, " tool_name=%s", msg.ToolName)
		}
		if msg.ToolCallID != "" {
			fmt.Fprintf(&b, " tool_call_id=%s", msg.ToolCallID)
		}
		if len(msg.ToolCalls) > 0 {
			fmt.Fprintf(&b, " tool_calls=%d", len(msg.ToolCalls))
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			content = "<empty>"
		}
		fmt.Fprintf(&b, "\n%s\n\n", clampRunes(content, 2000))
	}
	return b.String()
}

func emptyAsNone(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "<none>"
	}
	return value
}

func clampRunes(value string, max int) string {
	if max <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func (m RollingSummaryManager) recentMessages() int {
	if m.RecentMessages <= 0 {
		return defaultSummaryRecentMessages
	}
	return m.RecentMessages
}

func (m RollingSummaryManager) triggerMessages() int {
	if m.TriggerMessages <= 0 {
		return defaultSummaryTriggerMessages
	}
	return m.TriggerMessages
}

func (m RollingSummaryManager) keepMessages() int {
	if m.KeepMessages <= 0 {
		return defaultSummaryKeepMessages
	}
	return m.KeepMessages
}

func (m RollingSummaryManager) batchMessages() int {
	if m.BatchMessages <= 0 {
		return defaultSummaryBatchMessages
	}
	return m.BatchMessages
}

func (m RollingSummaryManager) maxSummaryChars() int {
	if m.MaxSummaryChars <= 0 {
		return defaultSummaryMaxChars
	}
	return m.MaxSummaryChars
}

func (m RollingSummaryManager) timeoutSeconds() int {
	if m.TimeoutSeconds <= 0 {
		return defaultSummaryTimeoutSeconds
	}
	return m.TimeoutSeconds
}

func (m RollingSummaryManager) summaryModelName() string {
	name := strings.TrimSpace(m.SummaryModelName)
	if name == "" {
		return "default"
	}
	return name
}

func (m RollingSummaryManager) log(ctx context.Context, sessionID string, eventType string, data map[string]any) {
	if m.Logger == nil {
		return
	}
	_ = m.Logger.Log(ctx, logx.Event{
		Time:    time.Now(),
		Type:    eventType,
		Session: sessionID,
		Data:    data,
	})
}
