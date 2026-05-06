package agent

import (
	"context"
	"errors"
	"fmt"

	"miniagent/internal/llm"
	"miniagent/internal/logx"
	"miniagent/internal/tools"
)

const defaultMaxTurns = 8

type RunOptions struct {
	DebugAPI    bool
	OnDelta     func(string)
	ApproveTool ApprovalFunc
	SessionID   string
}

type RunResult struct {
	Assistant   llm.Message
	Messages    []llm.Message
	ToolResults []ToolResult
}

type ToolResult struct {
	ToolCallID string
	ToolName   string
	Content    string
	IsError    bool
}

func (a *Agent) Run(ctx context.Context, messages []llm.Message, opts RunOptions) (RunResult, error) {
	if a.Client == nil {
		return RunResult{}, errors.New("agent client is nil")
	}
	if a.Dispatcher == nil {
		a.Dispatcher = NewDispatcher(a.Tools)
	}

	schemas := toolSchemas(a.Tools)
	var toolResults []ToolResult
	for turn := 1; turn <= a.maxTurns(); turn++ {
		modelMessages := messages
		if a.ContextManager != nil {
			modelMessages = a.ContextManager.Build(messages)
		}

		fmt.Printf("Agent turn %d: sending %d/%d messages to model\n", turn, len(modelMessages), len(messages))
		a.log(ctx, opts.SessionID, "model_turn", map[string]any{
			"turn":                turn,
			"message_count":       len(messages),
			"model_message_count": len(modelMessages),
			"tool_count":          len(schemas),
		})
		resp, err := a.Client.Generate(ctx, llm.GenerateRequest{
			Messages: modelMessages,
			Tools:    schemas,
			DebugAPI: opts.DebugAPI,
		})
		if err != nil {
			return RunResult{Messages: messages}, err
		}

		// Every model response becomes part of the full session history. Context
		// trimming only affects what we send to the model, not what we remember.
		messages = append(messages, resp.Assistant)
		if len(resp.ToolCalls) == 0 {
			if opts.OnDelta != nil {
				opts.OnDelta(resp.Assistant.Content)
			}
			return RunResult{
				Assistant:   resp.Assistant,
				Messages:    messages,
				ToolResults: toolResults,
			}, nil
		}

		fmt.Printf("Agent turn %d: executing %d tool call(s)\n", turn, len(resp.ToolCalls))
		var appendErr error
		var turnToolResults []ToolResult
		messages, turnToolResults, appendErr = a.appendToolResults(ctx, opts.SessionID, messages, resp.ToolCalls, opts.ApproveTool)
		toolResults = append(toolResults, turnToolResults...)
		if appendErr != nil {
			return RunResult{Messages: messages, ToolResults: toolResults}, appendErr
		}
	}

	return RunResult{Messages: messages, ToolResults: toolResults}, errors.New("agent loop reached max turns")
}

func (a *Agent) appendToolResults(ctx context.Context, sessionID string, messages []llm.Message, calls []llm.ToolCall, approve ApprovalFunc) ([]llm.Message, []ToolResult, error) {
	toolResults := make([]ToolResult, 0, len(calls))
	for _, call := range calls {
		// The model only names a tool and provides JSON arguments. Dispatcher
		// maps that request to the local Go implementation.
		a.log(ctx, sessionID, "tool_call", map[string]any{
			"tool":      call.Name,
			"arguments": string(call.Arguments),
		})
		result, found, err := a.Dispatcher.Execute(ctx, call, approve)
		if err != nil {
			return messages, toolResults, err
		}
		if !found {
			result = tools.Result{
				Content: "tool not found: " + call.Name,
				IsError: true,
			}
		}
		toolResults = append(toolResults, ToolResult{
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Content:    result.Content,
			IsError:    result.IsError,
		})
		a.log(ctx, sessionID, "tool_result", map[string]any{
			"tool":     call.Name,
			"is_error": result.IsError,
			"content":  result.Content,
		})

		// A tool result is fed back as a message so the next model turn can use
		// it to continue reasoning or produce the final answer.
		messages = append(messages, llm.Message{
			Role:       llm.RoleTool,
			Content:    result.Content,
			ToolCallID: call.ID,
			ToolName:   call.Name,
		})
	}

	return messages, toolResults, nil
}

func (a *Agent) log(ctx context.Context, sessionID string, eventType string, data map[string]any) {
	if a.Logger == nil {
		return
	}
	_ = a.Logger.Log(ctx, logx.Event{
		Type:    eventType,
		Session: sessionID,
		Data:    data,
	})
}

func (a *Agent) maxTurns() int {
	if a.MaxTurns <= 0 {
		return defaultMaxTurns
	}
	return a.MaxTurns
}

func toolSchemas(toolset []tools.Tool) []llm.ToolSchema {
	schemas := make([]llm.ToolSchema, 0, len(toolset))
	for _, tool := range toolset {
		schemas = append(schemas, tool.Schema())
	}
	return schemas
}
