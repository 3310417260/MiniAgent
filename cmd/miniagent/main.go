package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"miniagent/internal/agent"
	"miniagent/internal/llm"
	"miniagent/internal/prompt"
	"miniagent/internal/tools"
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
	}
	dispatcher := agent.NewDispatcher(toolset)
	debugAPI := envBool("MINIAGENT_DEBUG_API")

	userInput := strings.TrimSpace(strings.Join(os.Args[1:], " "))
	if userInput == "" {
		return runInteractive(client, dispatcher, toolset, debugAPI)
	}

	messages := newConversation()
	reply, _, err := ask(client, dispatcher, toolset, messages, userInput, debugAPI, func(delta string) {
		fmt.Print(delta)
	})
	if err != nil {
		return err
	}

	if reply.Content != "" {
		fmt.Println()
	}
	return nil
}

func runInteractive(client llm.Client, dispatcher *agent.Dispatcher, toolset []tools.Tool, debugAPI bool) error {
	scanner := bufio.NewScanner(os.Stdin)
	messages := newConversation()

	fmt.Println("MiniAgent interactive mode")
	fmt.Println("Type your message and press Enter. Commands: /help /history /debug-api /clear /exit")

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

		switch userInput {
		case "/exit", "/quit":
			fmt.Println("Bye.")
			return nil
		case "/help":
			fmt.Println("Enter any text to chat.")
			fmt.Println("/clear resets the in-memory conversation history.")
			fmt.Println("/debug-api toggles raw API request/response printing.")
			fmt.Println("/history shows the conversation history.")
			fmt.Println("/exit exits the program.")
			continue
		case "/clear":
			messages = newConversation()
			fmt.Println("Conversation cleared.")
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

		reply, updatedMessages, err := ask(client, dispatcher, toolset, messages, userInput, debugAPI, func(delta string) {
			fmt.Print(delta)
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
			continue
		}

		messages = updatedMessages
		if reply.Content != "" {
			fmt.Println()
		}
	}
}

func ask(client llm.Client, dispatcher *agent.Dispatcher, toolset []tools.Tool, messages []llm.Message, userInput string, debugAPI bool, onDelta func(string)) (llm.Message, []llm.Message, error) {
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
	req := llm.GenerateRequest{
		Messages: updatedMessages,
		Tools:    schemas,
		DebugAPI: debugAPI,
	}
	fmt.Printf("Sending %d messages to model\n", len(updatedMessages))
	resp, err := client.Generate(ctx, req)
	if err != nil {
		return llm.Message{}, messages, err
	}

	updatedMessages = append(updatedMessages, resp.Assistant)
	if len(resp.ToolCalls) == 0 {
		if onDelta != nil {
			onDelta(resp.Assistant.Content)
		}
		return resp.Assistant, updatedMessages, nil
	}

	// The model can only request tool calls. MiniAgent decides whether the tool
	// exists, runs the local Go code, and feeds the result back as tool messages.
	for _, call := range resp.ToolCalls {
		result, found, err := dispatcher.Execute(ctx, call)
		if err != nil {
			return llm.Message{}, messages, err
		}
		if !found {
			result = tools.Result{
				Content: "tool not found: " + call.Name,
				IsError: true,
			}
		}

		updatedMessages = append(updatedMessages, llm.Message{
			Role:       llm.RoleTool,
			Content:    result.Content,
			ToolCallID: call.ID,
			ToolName:   call.Name,
		})
	}

	fmt.Printf("Sending %d messages to model after tool result\n", len(updatedMessages))
	finalResp, err := client.Generate(ctx, llm.GenerateRequest{
		Messages: updatedMessages,
		Tools:    schemas,
		DebugAPI: debugAPI,
	})
	if err != nil {
		return llm.Message{}, messages, err
	}

	updatedMessages = append(updatedMessages, finalResp.Assistant)
	if onDelta != nil {
		onDelta(finalResp.Assistant.Content)
	}
	return finalResp.Assistant, updatedMessages, nil
}

func newConversation() []llm.Message {
	return []llm.Message{
		{
			Role:    llm.RoleSystem,
			Content: prompt.BaseSystemPrompt,
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
		names := make([]string, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			names = append(names, call.Name)
		}
		description += fmt.Sprintf(" tool_calls=%v", names)
	}
	if msg.ToolCallID != "" {
		description += fmt.Sprintf(" tool_call_id=%s tool_name=%s", msg.ToolCallID, msg.ToolName)
	}
	return description
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
