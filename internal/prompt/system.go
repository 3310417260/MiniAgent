package prompt

import "strings"

const BaseSystemPrompt = `You are MiniAgent, a concise and helpful coding assistant.
Answer clearly and directly.`

func BuildSystemPrompt(base string, agentsMD string) string {
	base = strings.TrimSpace(base)
	agentsMD = strings.TrimSpace(agentsMD)
	if agentsMD == "" {
		return base
	}

	return base + "\n\n# Project Instructions\n\n" + agentsMD
}
