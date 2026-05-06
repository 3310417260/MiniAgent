package agent

import (
	"miniagent/internal/contextx"
	"miniagent/internal/llm"
	"miniagent/internal/logx"
	"miniagent/internal/tools"
)

type Agent struct {
	Client         llm.Client
	Dispatcher     *Dispatcher
	ContextManager contextx.Manager
	Tools          []tools.Tool
	MaxTurns       int
	Logger         logx.Logger
}
