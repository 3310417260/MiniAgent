package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"miniagent/internal/agent"
	"miniagent/internal/contextx"
	"miniagent/internal/llm"
	"miniagent/internal/project"
	"miniagent/internal/prompt"
	"miniagent/internal/session"
	"miniagent/internal/tools"
)

const (
	maxAgentTurns   = 8
	defaultSession  = "default"
	sessionDir      = "sessions"
	sessionIDEnvVar = "MINIAGENT_SESSION"
	contextNEnvVar  = "MINIAGENT_CONTEXT_MESSAGES"
	defaultContextN = 40
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	client, err := llm.NewOpenAIClientFromEnv()
	if err != nil {
		return fmt.Errorf("init client: %w (set ZAI_API_KEY and optionally ZAI_BASE_URL / ZAI_MODEL)", err)
	}

	// toolset is the local capability list. The model only sees each tool's
	// schema; Dispatcher is what actually executes the Go implementation.
	toolset := []tools.Tool{
		tools.TimeTool{},
		tools.ListFilesTool{},
		tools.ReadFileTool{},
		tools.GrepTextTool{},
	}
	dispatcher := agent.NewDispatcher(toolset)
	debugAPI := envBool("MINIAGENT_DEBUG_API")
	store := session.NewJSONLStore(sessionDir)
	sessionID, err := initialSessionID()
	if err != nil {
		return err
	}
	systemPrompt, err := loadSystemPrompt()
	if err != nil {
		return err
	}
	contextManager, err := newContextManagerFromEnv()
	if err != nil {
		return err
	}

	userInput := strings.TrimSpace(strings.Join(os.Args[1:], " "))
	if userInput == "" {
		return runInteractive(client, dispatcher, toolset, store, contextManager, sessionID, systemPrompt, debugAPI)
	}

	ctx := context.Background()
	messages, err := loadSessionMessages(ctx, store, sessionID, systemPrompt)
	if err != nil {
		return err
	}
	startLen := len(messages)
	reply, updatedMessages, err := ask(client, dispatcher, toolset, contextManager, messages, userInput, debugAPI, func(delta string) {
		fmt.Print(delta)
	})
	if err != nil {
		return err
	}
	if err := appendSessionMessages(ctx, store, sessionID, updatedMessages[startLen:]); err != nil {
		return err
	}

	if reply.Content != "" {
		fmt.Println()
	}
	return nil
}

func runInteractive(client llm.Client, dispatcher *agent.Dispatcher, toolset []tools.Tool, store session.Store, contextManager contextx.Manager, sessionID string, systemPrompt string, debugAPI bool) error {
	scanner := bufio.NewScanner(os.Stdin)
	ctx := context.Background()
	messages, err := loadSessionMessages(ctx, store, sessionID, systemPrompt)
	if err != nil {
		return err
	}

	fmt.Println("MiniAgent interactive mode")
	fmt.Printf("Session: %s (%d messages)\n", sessionID, len(messages))
	fmt.Println("Type your message and press Enter. Commands: /help /history /session /debug-api /clear /exit")

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			fmt.Println()
			return nil
		}

		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "" {
			continue
		}
		if strings.HasPrefix(userInput, "/session") {
			nextSessionID, nextMessages, handled, err := handleSessionCommand(ctx, store, sessionID, systemPrompt, userInput)
			if err != nil {
				fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
			}
			if handled {
				if err == nil {
					sessionID = nextSessionID
					messages = nextMessages
				}
				continue
			}
		}

		switch userInput {
		case "/exit", "/quit":
			fmt.Println("Bye.")
			return nil
		case "/help":
			fmt.Println("Enter any text to chat.")
			fmt.Println("/clear resets the current session history.")
			fmt.Println("/debug-api toggles raw API request/response printing.")
			fmt.Println("/history shows the conversation history.")
			fmt.Println("/session shows the current session.")
			fmt.Println("/session list lists saved sessions.")
			fmt.Println("/session use <id> switches to a session, creating it if needed.")
			fmt.Println("/session new <id> creates and switches to a new empty session.")
			fmt.Println("/exit exits the program.")
			continue
		case "/clear":
			if err := store.Clear(ctx, sessionID); err != nil {
				fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
				continue
			}
			messages = newConversation(systemPrompt)
			fmt.Printf("Session %q cleared.\n", sessionID)
			continue
		case "/history":
			fmt.Printf("Conversation history: %d messages\n", len(messages))
			printMessages(messages)
			continue
		case "/debug-api":
			debugAPI = !debugAPI
			fmt.Printf("API debug output: %s\n", onOff(debugAPI))
			continue
		}

		startLen := len(messages)
		reply, updatedMessages, err := ask(client, dispatcher, toolset, contextManager, messages, userInput, debugAPI, func(delta string) {
			fmt.Print(delta)
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
			continue
		}
		if err := appendSessionMessages(ctx, store, sessionID, updatedMessages[startLen:]); err != nil {
			fmt.Fprintf(os.Stderr, "miniagent: persist session: %v\n", err)
		}

		messages = updatedMessages
		if reply.Content != "" {
			fmt.Println()
		}
	}
}

func ask(client llm.Client, dispatcher *agent.Dispatcher, toolset []tools.Tool, contextManager contextx.Manager, messages []llm.Message, userInput string, debugAPI bool, onDelta func(string)) (llm.Message, []llm.Message, error) {
	updatedMessages := append([]llm.Message(nil), messages...)
	updatedMessages = append(updatedMessages, llm.Message{
		Role:    llm.RoleUser,
		Content: userInput,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Tool schemas are the "menu" sent to the model. They describe available
	// tools, but they do not execute anything by themselves.
	schemas := toolSchemas(toolset)
	return runAgentLoop(ctx, client, dispatcher, contextManager, schemas, updatedMessages, debugAPI, onDelta)
}

func runAgentLoop(ctx context.Context, client llm.Client, dispatcher *agent.Dispatcher, contextManager contextx.Manager, schemas []llm.ToolSchema, messages []llm.Message, debugAPI bool, onDelta func(string)) (llm.Message, []llm.Message, error) {
	for turn := 1; turn <= maxAgentTurns; turn++ {
		modelMessages := contextManager.Build(messages)
		fmt.Printf("Agent turn %d: sending %d/%d messages to model\n", turn, len(modelMessages), len(messages))
		resp, err := client.Generate(ctx, llm.GenerateRequest{
			Messages: modelMessages,
			Tools:    schemas,
			DebugAPI: debugAPI,
		})
		if err != nil {
			return llm.Message{}, messages, err
		}

		// Every model response becomes part of the session. If it contains
		// tool_calls, this assistant message records what the model requested.
		messages = append(messages, resp.Assistant)
		if len(resp.ToolCalls) == 0 {
			if onDelta != nil {
				onDelta(resp.Assistant.Content)
			}
			return resp.Assistant, messages, nil
		}

		fmt.Printf("Agent turn %d: executing %d tool call(s)\n", turn, len(resp.ToolCalls))
		var appendErr error
		messages, appendErr = appendToolResults(ctx, dispatcher, messages, resp.ToolCalls)
		if appendErr != nil {
			return llm.Message{}, messages, appendErr
		}
	}

	return llm.Message{}, messages, errors.New("agent loop reached max turns")
}

func appendToolResults(ctx context.Context, dispatcher *agent.Dispatcher, messages []llm.Message, calls []llm.ToolCall) ([]llm.Message, error) {
	for _, call := range calls {
		// The model only names a tool and provides JSON arguments. Dispatcher
		// maps that request to the local Go implementation.
		result, found, err := dispatcher.Execute(ctx, call)
		if err != nil {
			return messages, err
		}
		if !found {
			result = tools.Result{
				Content: "tool not found: " + call.Name,
				IsError: true,
			}
		}

		// A tool result is fed back as a message so the next model turn can use
		// it to continue reasoning or produce the final answer.
		messages = append(messages, llm.Message{
			Role:       llm.RoleTool,
			Content:    result.Content,
			ToolCallID: call.ID,
			ToolName:   call.Name,
		})
	}

	return messages, nil
}

func newConversation(systemPrompt string) []llm.Message {
	return []llm.Message{
		{
			Role:    llm.RoleSystem,
			Content: systemPrompt,
		},
	}
}

func printMessages(messages []llm.Message) {
	for i, msg := range messages {
		fmt.Printf("%02d. role=%s %s\n", i+1, msg.Role, describeMessage(msg))
	}
}

func describeMessage(msg llm.Message) string {
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		content = "<empty>"
	}

	description := fmt.Sprintf("content=%q", preview(content, 120))
	if len(msg.ToolCalls) > 0 {
		description += fmt.Sprintf(" tool_calls=%v", describeToolCalls(msg.ToolCalls))
	}
	if msg.ToolCallID != "" {
		description += fmt.Sprintf(" tool_call_id=%s tool_name=%s", msg.ToolCallID, msg.ToolName)
	}
	return description
}

func describeToolCalls(calls []llm.ToolCall) []string {
	descriptions := make([]string, 0, len(calls))
	for _, call := range calls {
		descriptions = append(descriptions, fmt.Sprintf("%s %s", call.Name, compactJSON(call.Arguments)))
	}
	return descriptions
}

func compactJSON(raw json.RawMessage) string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return "{}"
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return string(raw)
	}
	return string(encoded)
}

func preview(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

func toolSchemas(toolset []tools.Tool) []llm.ToolSchema {
	schemas := make([]llm.ToolSchema, 0, len(toolset))
	for _, tool := range toolset {
		schemas = append(schemas, tool.Schema())
	}
	return schemas
}

func envBool(key string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func initialSessionID() (string, error) {
	id := strings.TrimSpace(os.Getenv(sessionIDEnvVar))
	if id == "" {
		id = defaultSession
	}
	if !session.ValidID(id) {
		return "", fmt.Errorf("invalid %s %q", sessionIDEnvVar, id)
	}
	return id, nil
}

func loadSystemPrompt() (string, error) {
	agentsPath, err := project.FindAGENTSMD(".")
	if err != nil {
		return prompt.BuildSystemPrompt(prompt.BaseSystemPrompt, ""), nil
	}

	content, err := os.ReadFile(agentsPath)
	if err != nil {
		return "", fmt.Errorf("read AGENTS.md: %w", err)
	}
	return prompt.BuildSystemPrompt(prompt.BaseSystemPrompt, string(content)), nil
}

func newContextManagerFromEnv() (contextx.Manager, error) {
	maxMessages := defaultContextN
	value := strings.TrimSpace(os.Getenv(contextNEnvVar))
	if value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", contextNEnvVar, err)
		}
		maxMessages = parsed
	}
	return contextx.RecentNManager{MaxMessages: maxMessages}, nil
}

func handleSessionCommand(ctx context.Context, store session.Store, currentID string, systemPrompt string, input string) (string, []llm.Message, bool, error) {
	fields := strings.Fields(input)
	if len(fields) == 0 || fields[0] != "/session" {
		return currentID, nil, false, nil
	}

	if len(fields) == 1 || fields[1] == "current" {
		messages, err := loadSessionMessages(ctx, store, currentID, systemPrompt)
		if err != nil {
			return currentID, nil, true, err
		}
		fmt.Printf("Current session: %s (%d messages)\n", currentID, len(messages))
		return currentID, messages, true, nil
	}

	switch fields[1] {
	case "list":
		ids, err := store.List(ctx)
		if err != nil {
			return currentID, nil, true, err
		}
		printSessions(ids, currentID)
		messages, err := loadSessionMessages(ctx, store, currentID, systemPrompt)
		return currentID, messages, true, err
	case "use":
		if len(fields) != 3 {
			return currentID, nil, true, errors.New("usage: /session use <id>")
		}
		nextID := fields[2]
		messages, err := loadSessionMessages(ctx, store, nextID, systemPrompt)
		if err != nil {
			return currentID, nil, true, err
		}
		fmt.Printf("Switched to session: %s (%d messages)\n", nextID, len(messages))
		return nextID, messages, true, nil
	case "new":
		if len(fields) != 3 {
			return currentID, nil, true, errors.New("usage: /session new <id>")
		}
		nextID := fields[2]
		exists, err := sessionExists(ctx, store, nextID)
		if err != nil {
			return currentID, nil, true, err
		}
		if exists {
			return currentID, nil, true, fmt.Errorf("session %q already exists", nextID)
		}
		if err := store.Clear(ctx, nextID); err != nil {
			return currentID, nil, true, err
		}
		messages := newConversation(systemPrompt)
		fmt.Printf("Created and switched to session: %s\n", nextID)
		return nextID, messages, true, nil
	default:
		return currentID, nil, true, errors.New("usage: /session [current|list|use <id>|new <id>]")
	}
}

func loadSessionMessages(ctx context.Context, store session.Store, sessionID string, systemPrompt string) ([]llm.Message, error) {
	history, err := store.Load(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// System prompt is assembled at runtime so future prompt/AGENTS.md changes
	// can apply when loading an old session.
	messages := newConversation(systemPrompt)
	for _, msg := range history {
		if msg.Role == llm.RoleSystem {
			continue
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func appendSessionMessages(ctx context.Context, store session.Store, sessionID string, messages []llm.Message) error {
	for _, msg := range messages {
		if msg.Role == llm.RoleSystem {
			continue
		}
		if err := store.Append(ctx, sessionID, msg); err != nil {
			return err
		}
	}
	return nil
}

func sessionExists(ctx context.Context, store session.Store, sessionID string) (bool, error) {
	if !session.ValidID(sessionID) {
		return false, fmt.Errorf("invalid session id %q", sessionID)
	}
	ids, err := store.List(ctx)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == sessionID {
			return true, nil
		}
	}
	return false, nil
}

func printSessions(ids []string, currentID string) {
	if len(ids) == 0 {
		fmt.Println("No saved sessions.")
		return
	}
	fmt.Println("Sessions:")
	for _, id := range ids {
		prefix := "  "
		if id == currentID {
			prefix = "* "
		}
		fmt.Println(prefix + id)
	}
}
