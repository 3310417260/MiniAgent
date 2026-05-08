package prompt

import "strings"

const BaseSystemPrompt = `You are MiniAgent, a concise and helpful coding assistant.
Answer clearly and directly.
When a tool result contains JSON with ok=false, read error_type, message, recoverable, and suggested_next_step before deciding whether to retry, inspect, or stop.`

func BuildSystemPrompt(base string, agentsMD string) string {
	base = strings.TrimSpace(base)
	agentsMD = strings.TrimSpace(agentsMD)
	if agentsMD == "" {
		return base
	}

	return base + "\n\n# Project Instructions\n\n" + agentsMD
}
