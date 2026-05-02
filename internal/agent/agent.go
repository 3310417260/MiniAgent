package agent

import (
	"miniagent/internal/contextx"
	"miniagent/internal/llm"
	"miniagent/internal/session"
	"miniagent/internal/tools"
)

type Agent struct {
	Client         llm.Client
	Dispatcher     *Dispatcher
	Store          session.Store
	ContextManager contextx.Manager
	Tools          []tools.Tool
}
