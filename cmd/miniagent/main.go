package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"miniagent/internal/agent"
	"miniagent/internal/contextx"
	"miniagent/internal/llm"
	"miniagent/internal/logx"
	"miniagent/internal/plan"
	"miniagent/internal/project"
	"miniagent/internal/prompt"
	"miniagent/internal/session"
	"miniagent/internal/skill"
	"miniagent/internal/tools"
)

const (
	maxAgentTurns          = 8
	defaultSession         = "default"
	sessionDir             = "sessions"
	skillsDir              = "skills"
	logPath                = "logs/miniagent.jsonl"
	sessionIDEnvVar        = "MINIAGENT_SESSION"
	contextNEnvVar         = "MINIAGENT_CONTEXT_MESSAGES"
	summaryModelEnvVar     = "MINIAGENT_SUMMARY_MODEL"
	summaryBaseURLEnvVar   = "MINIAGENT_SUMMARY_BASE_URL"
	summaryAPIKeyEnvVar    = "MINIAGENT_SUMMARY_API_KEY_ENV"
	summaryTriggerEnvVar   = "MINIAGENT_SUMMARY_TRIGGER_MESSAGES"
	summaryKeepEnvVar      = "MINIAGENT_SUMMARY_KEEP_MESSAGES"
	summaryBatchEnvVar     = "MINIAGENT_SUMMARY_BATCH_MESSAGES"
	summaryMaxCharsEnvVar  = "MINIAGENT_SUMMARY_MAX_CHARS"
	summaryTimeoutEnvVar   = "MINIAGENT_SUMMARY_TIMEOUT_SECONDS"
	defaultContextN        = 20
	defaultSummaryTrigger  = 40
	defaultSummaryKeep     = 20
	defaultSummaryBatch    = 5
	defaultSummaryMaxChars = 3000
	defaultSummaryTimeout  = 20
	defaultLogTail         = 20
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
		return fmt.Errorf("init client: %w (set DEEPSEEK_API_KEY or ZAI_API_KEY and optionally matching BASE_URL / MODEL)", err)
	}

	planState := plan.NewState()
	plannerTools := []tools.Tool{
		tools.SetPlanTool{State: planState},
	}
	skillStore := skill.NewStore(skillsDir)

	// toolset is the local capability list. The model only sees each tool's
	// schema; Dispatcher is what actually executes the Go implementation.
	toolset := []tools.Tool{
		tools.TimeTool{},
		tools.ListFilesTool{},
		tools.ReadFileTool{},
		tools.GrepTextTool{},
		tools.WriteFileTool{},
		tools.EditFileTool{},
		tools.RunShellTool{},
		tools.RunSkillScriptTool{Store: skillStore},
	}
	debugAPI := envBool("MINIAGENT_DEBUG_API")
	logger := logx.NewJSONLLogger(logPath)
	store := session.NewJSONLStore(sessionDir)
	summaryStore := contextx.NewFileSummaryStore(filepath.Join(sessionDir, "summaries"))
	sessionID, err := initialSessionID()
	if err != nil {
		return err
	}
	systemPrompt, err := loadSystemPrompt()
	if err != nil {
		return err
	}
	// Planner rules are loaded as a skill so the planning behavior can be
	// adjusted by editing skills/planner/SKILL.md instead of recompiling Go.
	plannerPrompt := loadPlannerSystemPrompt(skillStore)
	summaryClient, summaryModelName, err := newSummaryClientFromEnv(client)
	if err != nil {
		return err
	}
	contextManager, err := newContextManagerFromEnv(summaryClient, summaryStore, logger, summaryModelName)
	if err != nil {
		return err
	}
	runtime := &agent.Agent{
		Client:         client,
		Dispatcher:     agent.NewDispatcher(toolset),
		ContextManager: contextManager,
		Tools:          toolset,
		MaxTurns:       maxAgentTurns,
		Logger:         logger,
	}
	planner := &agent.Agent{
		Client:     client,
		Dispatcher: agent.NewDispatcher(plannerTools),
		ContextManager: contextx.ReadOnlySummaryManager{
			Store:          summaryStore,
			RecentMessages: defaultContextN,
		},
		Tools:    plannerTools,
		MaxTurns: 4,
		Logger:   logger,
	}

	userInput := strings.TrimSpace(strings.Join(os.Args[1:], " "))
	if userInput == "" {
		return runInteractive(runtime, planner, planState, logger, store, summaryStore, skillStore, sessionID, systemPrompt, plannerPrompt, debugAPI)
	}

	ctx := context.Background()
	messages, err := loadSessionMessages(ctx, store, sessionID, systemPrompt)
	if err != nil {
		return err
	}
	startLen := len(messages)
	reply, updatedMessages, err := ask(runtime, messages, userInput, debugAPI, sessionID, nil, func(delta string) {
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

func runInteractive(runtime *agent.Agent, planner *agent.Agent, planState *plan.State, logger *logx.JSONLLogger, store session.Store, summaryStore contextx.SummaryStore, skillStore skill.Store, sessionID string, systemPrompt string, plannerPrompt string, debugAPI bool) error {
	scanner := bufio.NewScanner(os.Stdin)
	ctx := context.Background()
	messages, err := loadSessionMessages(ctx, store, sessionID, systemPrompt)
	if err != nil {
		return err
	}

	fmt.Println("MiniAgent interactive mode")
	fmt.Printf("Session: %s (%d messages)\n", sessionID, len(messages))
	fmt.Println("Type your message and press Enter. Commands: /help /chat /task /plan /summary /skill-route /skill-load /skill-scripts /skill-manifest /skills /skill /logs /history /session /debug-api /clear /exit")

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
		if strings.HasPrefix(userInput, "/chat") {
			chatInput := strings.TrimSpace(strings.TrimPrefix(userInput, "/chat"))
			if chatInput == "" {
				fmt.Println("usage: /chat <message>")
				continue
			}

			startLen := len(messages)
			reply, updatedMessages, err := chatStream(runtime.Client, runtime.ContextManager, logger, sessionID, messages, chatInput, debugAPI, func(delta string) {
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
			continue
		}
		if strings.HasPrefix(userInput, "/task") {
			taskInput := strings.TrimSpace(strings.TrimPrefix(userInput, "/task"))
			if taskInput == "" {
				fmt.Println("usage: /task <goal>")
				continue
			}

			startLen := len(messages)
			reply, updatedMessages, err := planThenExecute(scanner, planner, runtime, planState, logger, skillStore, sessionID, messages, taskInput, plannerPrompt, debugAPI, cliApproveTool(scanner, logger, sessionID), func(delta string) {
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
			continue
		}

		switch userInput {
		case "/exit", "/quit":
			fmt.Println("Bye.")
			return nil
		case "/help":
			fmt.Println("Enter any text to chat.")
			fmt.Println("/chat <message> streams a plain chat response without tools.")
			fmt.Println("/task <goal> asks the model to plan first, then waits for your approval before executing.")
			fmt.Println("/plan shows the current task plan.")
			fmt.Println("/summary shows the current rolling summary for this session.")
			fmt.Println("/skill-route <task> asks the model to choose one skill from name + description only.")
			fmt.Println("/skill-load <task> routes, loads the selected SKILL.md body, and asks for dry-run guidance without tools.")
			fmt.Println("/skill-scripts <skill> lists scripts packaged with one skill without running them.")
			fmt.Println("/skill-manifest <skill> shows local script execution permissions for one skill.")
			fmt.Println("/skills lists local SKILL.md packages.")
			fmt.Println("/skill <name> shows one local skill prompt preview.")
			fmt.Println("/logs [all|session <id>|type <event>|errors|tail <n>] shows structured JSONL events.")
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
			if err := summaryStore.Clear(ctx, sessionID); err != nil {
				fmt.Fprintf(os.Stderr, "miniagent: clear summary: %v\n", err)
				continue
			}
			messages = newConversation(systemPrompt)
			fmt.Printf("Session %q cleared.\n", sessionID)
			continue
		case "/history":
			fmt.Printf("Conversation history: %d messages\n", len(messages))
			printMessages(messages)
			continue
		case "/plan":
			fmt.Println(planState.String())
			continue
		case "/summary":
			if err := printSummary(ctx, summaryStore, sessionID); err != nil {
				fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
			}
			continue
		case "/skills":
			if err := printSkills(skillStore); err != nil {
				fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
			}
			continue
		case "/debug-api":
			debugAPI = !debugAPI
			fmt.Printf("API debug output: %s\n", onOff(debugAPI))
			continue
		}
		if strings.HasPrefix(userInput, "/skill-route") {
			taskInput := strings.TrimSpace(strings.TrimPrefix(userInput, "/skill-route"))
			if taskInput == "" {
				fmt.Println("usage: /skill-route <task>")
				continue
			}
			selection, err := routeSkill(runtime.Client, logger, skillStore, taskInput, debugAPI, sessionID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
				continue
			}
			fmt.Println(selection.String())
			if selection.UsesSkill() {
				selectedSkill, err := skillStore.Load(selection.Name)
				if err != nil {
					fmt.Fprintf(os.Stderr, "miniagent: load selected skill: %v\n", err)
					continue
				}
				fmt.Printf("Path: %s\n", selectedSkill.Path)
				fmt.Println("Selected skill body was not injected or executed in /skill-route.")
			}
			continue
		}
		if strings.HasPrefix(userInput, "/skill-load") {
			taskInput := strings.TrimSpace(strings.TrimPrefix(userInput, "/skill-load"))
			if taskInput == "" {
				fmt.Println("usage: /skill-load <task>")
				continue
			}
			selection, err := routeSkill(runtime.Client, logger, skillStore, taskInput, debugAPI, sessionID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
				continue
			}
			fmt.Println(selection.String())
			if !selection.UsesSkill() {
				fmt.Println("No skill body loaded because the router selected none.")
				continue
			}

			loadedSkill, reply, err := loadSelectedSkillForTask(runtime.Client, logger, skillStore, selection, taskInput, debugAPI, sessionID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
				continue
			}
			fmt.Printf("Path: %s\n", loadedSkill.Path)
			fmt.Println("Loaded selected SKILL.md body for dry-run guidance. No tools or scripts were exposed.")
			if strings.TrimSpace(reply.Content) != "" {
				fmt.Println()
				fmt.Println(strings.TrimSpace(reply.Content))
			}
			continue
		}
		if strings.HasPrefix(userInput, "/skill-scripts") {
			if err := printSkillScripts(skillStore, userInput); err != nil {
				fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
			}
			continue
		}
		if strings.HasPrefix(userInput, "/skill-manifest") {
			if err := printSkillManifest(skillStore, userInput); err != nil {
				fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
			}
			continue
		}
		if strings.HasPrefix(userInput, "/skill") {
			if err := printSkill(skillStore, userInput); err != nil {
				fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
			}
			continue
		}
		if strings.HasPrefix(userInput, "/logs") {
			filter, err := parseLogCommand(userInput, sessionID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
				continue
			}
			if err := printRecentLogs(logger.Path(), filter); err != nil {
				fmt.Fprintf(os.Stderr, "miniagent: %v\n", err)
			}
			continue
		}

		startLen := len(messages)
		reply, updatedMessages, err := ask(runtime, messages, userInput, debugAPI, sessionID, cliApproveTool(scanner, logger, sessionID), func(delta string) {
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

func planThenExecute(scanner *bufio.Scanner, planner *agent.Agent, runtime *agent.Agent, planState *plan.State, logger logx.Logger, skillStore skill.Store, sessionID string, messages []llm.Message, task string, plannerPrompt string, debugAPI bool, approve agent.ApprovalFunc, onDelta func(string)) (llm.Message, []llm.Message, error) {
	taskSkill := routeTaskSkill(runtime.Client, logger, skillStore, task, debugAPI, sessionID)
	if taskSkill.Skill.Name != "" {
		fmt.Println()
		fmt.Printf("Task skill selected: %s\n", taskSkill.Skill.Name)
		if taskSkill.Selection.Reason != "" {
			fmt.Printf("Reason: %s\n", taskSkill.Selection.Reason)
		}
		fmt.Println("Loaded selected SKILL.md for planning and execution guidance. Scripts still require manifest permission and approval.")
	}

	feedback := ""
	for {
		planState.Clear()
		if err := generatePlan(planner, task, feedback, plannerPrompt, taskSkill, debugAPI, sessionID); err != nil {
			return llm.Message{}, messages, err
		}
		if planState.Empty() {
			return llm.Message{}, messages, errors.New("planner did not produce a plan")
		}
		logEvent(context.Background(), logger, sessionID, "plan_generated", map[string]any{
			"title":      planState.Title,
			"step_count": len(planState.Steps),
		})

		fmt.Println()
		fmt.Println(planState.String())
		fmt.Println()
		fmt.Print("Approve plan? type yes to execute, revise <feedback> to modify, or cancel: ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return llm.Message{}, messages, err
			}
			return llm.Message{}, messages, errors.New("plan approval cancelled")
		}

		answer := strings.TrimSpace(scanner.Text())
		lower := strings.ToLower(answer)
		switch {
		case lower == "yes" || lower == "y":
			logEvent(context.Background(), logger, sessionID, "plan_approved", map[string]any{
				"title":      planState.Title,
				"step_count": len(planState.Steps),
			})
			return executePlanSteps(runtime, planState, logger, sessionID, messages, task, taskSkill, debugAPI, approve, onDelta)
		case lower == "cancel":
			logEvent(context.Background(), logger, sessionID, "plan_cancelled", map[string]any{
				"title": planState.Title,
			})
			return llm.Message{}, messages, errors.New("task cancelled before execution")
		case strings.HasPrefix(lower, "revise "):
			feedback = strings.TrimSpace(answer[len("revise "):])
			if feedback == "" {
				fmt.Println("revision feedback is empty")
			} else {
				logEvent(context.Background(), logger, sessionID, "plan_revised", map[string]any{
					"feedback": feedback,
				})
			}
		default:
			fmt.Println("Please type yes, revise <feedback>, or cancel.")
		}
	}
}

func executePlanSteps(runtime *agent.Agent, planState *plan.State, logger logx.Logger, sessionID string, messages []llm.Message, task string, taskSkill taskSkillContext, debugAPI bool, approve agent.ApprovalFunc, onDelta func(string)) (llm.Message, []llm.Message, error) {
	updatedMessages := messages
	var lastReply llm.Message

	for i, step := range planState.Steps {
		stepIndex := i + 1
		if err := planState.Update(stepIndex, plan.StatusInProgress, ""); err != nil {
			return lastReply, updatedMessages, err
		}
		logPlanStep(logger, sessionID, stepIndex, plan.StatusInProgress, step.Text)
		fmt.Println()
		fmt.Println(planState.String())
		fmt.Println()

		stepPrompt := fmt.Sprintf("Execute only step %d of this approved plan. Do not execute later steps yet.\n\nTask:\n%s\n\nCurrent step:\n%s\n\nApproved plan:\n%s", stepIndex, task, step.Text, planState.String())
		if taskSkill.Skill.Name != "" {
			stepPrompt += "\n\nSelected skill guidance for this task:\n" + taskSkillPrompt(taskSkill)
		}
		result, err := askResult(runtime, updatedMessages, stepPrompt, debugAPI, sessionID, approve, onDelta)
		if err != nil {
			_ = planState.Update(stepIndex, plan.StatusFailed, "")
			logPlanStep(logger, sessionID, stepIndex, plan.StatusFailed, step.Text)
			return lastReply, updatedMessages, err
		}
		if failedTool, ok := firstToolError(result.ToolResults); ok {
			updatedMessages = result.Messages
			lastReply = result.Assistant
			_ = planState.Update(stepIndex, plan.StatusFailed, "")
			logPlanStep(logger, sessionID, stepIndex, plan.StatusFailed, step.Text)
			return lastReply, updatedMessages, fmt.Errorf("step %d failed because tool %s returned an error: %s", stepIndex, failedTool.ToolName, preview(failedTool.Content, 200))
		}

		lastReply = result.Assistant
		updatedMessages = result.Messages
		if err := planState.Update(stepIndex, plan.StatusDone, ""); err != nil {
			return lastReply, updatedMessages, err
		}
		logPlanStep(logger, sessionID, stepIndex, plan.StatusDone, step.Text)
	}

	fmt.Println()
	fmt.Println(planState.String())
	return lastReply, updatedMessages, nil
}

func firstToolError(results []agent.ToolResult) (agent.ToolResult, bool) {
	for _, result := range results {
		if result.IsError && !result.Recoverable {
			return result, true
		}
	}
	return agent.ToolResult{}, false
}

type taskSkillContext struct {
	Selection skill.Selection
	Skill     skill.Skill
}

func routeTaskSkill(client llm.Client, logger logx.Logger, store skill.Store, task string, debugAPI bool, sessionID string) taskSkillContext {
	selection, err := routeSkillWithOptions(client, logger, store, task, debugAPI, sessionID, skillRouteOptions{
		ExcludeNames: map[string]bool{
			"planner": true,
		},
	})
	if err != nil {
		logEvent(context.Background(), logger, sessionID, "skill_route_failed", map[string]any{
			"error": err.Error(),
		})
		return taskSkillContext{}
	}
	if !selection.UsesSkill() {
		logEvent(context.Background(), logger, sessionID, "task_skill_none", map[string]any{
			"reason": selection.Reason,
		})
		return taskSkillContext{Selection: selection}
	}

	selectedSkill, err := store.Load(selection.Name)
	if err != nil {
		logEvent(context.Background(), logger, sessionID, "skill_load_failed", map[string]any{
			"name":  selection.Name,
			"error": err.Error(),
		})
		return taskSkillContext{Selection: selection}
	}
	logEvent(context.Background(), logger, sessionID, "task_skill_loaded", map[string]any{
		"name":          selectedSkill.Name,
		"path":          selectedSkill.Path,
		"content_chars": len([]rune(selectedSkill.Content)),
		"reason":        selection.Reason,
	})
	return taskSkillContext{
		Selection: selection,
		Skill:     selectedSkill,
	}
}

func taskSkillPrompt(ctx taskSkillContext) string {
	if ctx.Skill.Name == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Selected skill: %s\n", ctx.Skill.Name)
	if ctx.Selection.Reason != "" {
		fmt.Fprintf(&b, "Selection reason: %s\n", ctx.Selection.Reason)
	}
	b.WriteString("\n")
	b.WriteString(ctx.Skill.Prompt())
	b.WriteString("\n\nImportant: Loading a skill only provides instructions. Do not assume any script has run. If a script is needed, use run_skill_script and respect manifest permissions and approval.")
	return b.String()
}

func generatePlan(planner *agent.Agent, task string, feedback string, plannerPrompt string, taskSkill taskSkillContext, debugAPI bool, sessionID string) error {
	planningPrompt := fmt.Sprintf("Create a concise execution plan for this task. You must call set_plan with a short title and 3 to 6 actionable steps. Do not execute the task yet.\n\nTask:\n%s", task)
	if strings.TrimSpace(feedback) != "" {
		planningPrompt += "\n\nUser revision feedback:\n" + feedback
	}
	if taskSkill.Skill.Name != "" {
		planningPrompt += "\n\nSelected skill guidance for planning:\n" + taskSkillPrompt(taskSkill)
	}

	planningMessages := []llm.Message{
		{
			Role:    llm.RoleSystem,
			Content: plannerPrompt,
		},
		{
			Role:    llm.RoleUser,
			Content: planningPrompt,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, err := planner.Run(ctx, planningMessages, agent.RunOptions{
		DebugAPI:          debugAPI,
		SessionID:         sessionID,
		StopAfterToolCall: true,
	})
	return err
}

func routeSkill(client llm.Client, logger logx.Logger, store skill.Store, task string, debugAPI bool, sessionID string) (skill.Selection, error) {
	return routeSkillWithOptions(client, logger, store, task, debugAPI, sessionID, skillRouteOptions{})
}

type skillRouteOptions struct {
	ExcludeNames map[string]bool
}

func routeSkillWithOptions(client llm.Client, logger logx.Logger, store skill.Store, task string, debugAPI bool, sessionID string, opts skillRouteOptions) (skill.Selection, error) {
	catalog, err := store.Catalog()
	if err != nil {
		return skill.Selection{}, err
	}
	catalog = filterSkillCatalog(catalog, opts.ExcludeNames)
	if len(catalog) == 0 {
		return skill.Selection{}, errors.New("no skills available")
	}

	selection := &skill.Selection{}
	toolset := []tools.Tool{
		tools.SelectSkillTool{
			State:        selection,
			AllowedNames: catalogNames(catalog),
		},
	}
	router := &agent.Agent{
		Client:     client,
		Dispatcher: agent.NewDispatcher(toolset),
		Tools:      toolset,
		MaxTurns:   3,
		Logger:     logger,
	}

	messages := []llm.Message{
		{
			Role:    llm.RoleSystem,
			Content: skillRouterSystemPrompt(),
		},
		{
			Role:    llm.RoleUser,
			Content: fmt.Sprintf("User task:\n%s\n\nSkill catalog:\n%s", task, formatSkillCatalog(catalog)),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	_, err = router.Run(ctx, messages, agent.RunOptions{
		DebugAPI:  debugAPI,
		SessionID: sessionID,
	})
	if err != nil {
		return *selection, err
	}
	if selection.Empty() {
		return *selection, errors.New("skill router did not select a skill")
	}

	logEvent(ctx, logger, sessionID, "skill_routed", map[string]any{
		"name":   selection.Name,
		"reason": selection.Reason,
	})
	return *selection, nil
}

func filterSkillCatalog(catalog []skill.CatalogEntry, excluded map[string]bool) []skill.CatalogEntry {
	if len(excluded) == 0 {
		return catalog
	}
	filtered := make([]skill.CatalogEntry, 0, len(catalog))
	for _, item := range catalog {
		if excluded[item.Name] {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func loadSelectedSkillForTask(client llm.Client, logger logx.Logger, store skill.Store, selection skill.Selection, task string, debugAPI bool, sessionID string) (skill.Skill, llm.Message, error) {
	if !selection.UsesSkill() {
		return skill.Skill{}, llm.Message{}, errors.New("no selected skill to load")
	}

	selectedSkill, err := store.Load(selection.Name)
	if err != nil {
		return skill.Skill{}, llm.Message{}, err
	}

	messages := []llm.Message{
		{
			Role:    llm.RoleSystem,
			Content: skillLoadSystemPrompt(),
		},
		{
			Role: llm.RoleUser,
			Content: fmt.Sprintf("User task:\n%s\n\nSelected skill:\n%s\n\nSelection reason:\n%s\n\nLoaded SKILL.md body:\n%s",
				task,
				selectedSkill.Name,
				selection.Reason,
				selectedSkill.Prompt(),
			),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logEvent(ctx, logger, sessionID, "skill_loaded", map[string]any{
		"name":          selectedSkill.Name,
		"path":          selectedSkill.Path,
		"content_chars": len([]rune(selectedSkill.Content)),
	})

	resp, err := client.Generate(ctx, llm.GenerateRequest{
		Messages: messages,
		DebugAPI: debugAPI,
	})
	if err != nil {
		return skill.Skill{}, llm.Message{}, err
	}
	return selectedSkill, resp.Assistant, nil
}

func skillRouterSystemPrompt() string {
	return strings.TrimSpace(`
You are MiniAgent's skill router.

Your only job is to choose the most relevant local skill for the user task.

You must call select_skill exactly once.

Rules:
- Choose only from the provided skill catalog.
- Use name "none" if no skill is clearly relevant.
- Do not solve the task.
- Do not create a plan.
- Do not claim that a skill body, script, file, or tool has been executed.
- Base your choice only on each skill's name and description.
`)
}

func skillLoadSystemPrompt() string {
	return strings.TrimSpace(`
You are MiniAgent's skill loader dry run.

The selected SKILL.md body has been loaded into this conversation so you can explain how it would guide the task.

Rules:
- Do not execute the task.
- Do not claim that files, tools, scripts, or commands have been executed.
- Do not ask to run Python, shell, or any script.
- Summarize which parts of the loaded skill are relevant to the task.
- Mention the next safe integration step in MiniAgent terms.
`)
}

func formatSkillCatalog(catalog []skill.CatalogEntry) string {
	var b strings.Builder
	for _, item := range catalog {
		description := strings.TrimSpace(item.Description)
		if description == "" {
			description = "(no description)"
		}
		fmt.Fprintf(&b, "- %s: %s\n", item.Name, description)
	}
	fmt.Fprintf(&b, "- %s: no skill is relevant\n", skill.None)
	return strings.TrimSpace(b.String())
}

func catalogNames(catalog []skill.CatalogEntry) []string {
	names := make([]string, 0, len(catalog))
	for _, item := range catalog {
		names = append(names, item.Name)
	}
	return names
}

func loadPlannerSystemPrompt(store skill.Store) string {
	plannerSkill, err := store.Load("planner")
	if err != nil {
		return fallbackPlannerSystemPrompt()
	}
	return plannerSkill.Prompt()
}

func fallbackPlannerSystemPrompt() string {
	return strings.TrimSpace(`
You are MiniAgent's planner. Your job is only to create or revise a plan for user approval.

You must always call set_plan with a short title and 3 to 6 actionable steps.
Do not execute the task. Do not claim that any file, command, or test has already been changed or run.

Available execution capabilities after the user approves the plan:
- list_files: list workspace files and directories.
- read_file: read UTF-8 workspace files.
- grep_text: search literal text in workspace files.
- write_file: create or overwrite workspace files with explicit approval.
- edit_file: replace one unique old_text with new_text in a workspace file with explicit approval.
- run_shell: run only allowlisted verification commands with explicit approval.
- run_skill_script: run a small allowlisted skill script with explicit approval.

run_shell allowlist:
- go version
- go test ./...
- go test ./cmd/...
- go test ./internal/...
- git status --short
- git diff --stat

Planning rules:
- Do not plan to use sed, cat, rm, curl, sh, bash, python, git push, or arbitrary shell commands.
- For file edits, plan to read or verify the relevant file first, then use edit_file with exact old_text and new_text.
- If the requested old text may not exist, include a verification step before editing.
- For creating files, plan to use write_file.
- For tests, plan to use run_shell with go test ./... or a narrower allowlisted go test command.
- For skill script demos, plan to use run_skill_script only for allowlisted scripts such as demo/scripts/echo_args.sh.
- Keep steps concrete and executable by MiniAgent's available capabilities.
`)
}

func ask(runtime *agent.Agent, messages []llm.Message, userInput string, debugAPI bool, sessionID string, approve agent.ApprovalFunc, onDelta func(string)) (llm.Message, []llm.Message, error) {
	result, err := askResult(runtime, messages, userInput, debugAPI, sessionID, approve, onDelta)
	if err != nil {
		return llm.Message{}, messages, err
	}
	return result.Assistant, result.Messages, nil
}

func askResult(runtime *agent.Agent, messages []llm.Message, userInput string, debugAPI bool, sessionID string, approve agent.ApprovalFunc, onDelta func(string)) (agent.RunResult, error) {
	updatedMessages := append([]llm.Message(nil), messages...)
	updatedMessages = append(updatedMessages, llm.Message{
		Role:    llm.RoleUser,
		Content: userInput,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result, err := runtime.Run(ctx, updatedMessages, agent.RunOptions{
		DebugAPI:    debugAPI,
		ApproveTool: approve,
		OnDelta:     onDelta,
		SessionID:   sessionID,
	})
	if err != nil {
		return agent.RunResult{Messages: messages}, err
	}
	return result, nil
}

func chatStream(client llm.Client, contextManager contextx.Manager, logger logx.Logger, sessionID string, messages []llm.Message, userInput string, debugAPI bool, onDelta func(string)) (llm.Message, []llm.Message, error) {
	updatedMessages := append([]llm.Message(nil), messages...)
	updatedMessages = append(updatedMessages, llm.Message{
		Role:    llm.RoleUser,
		Content: userInput,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// /chat is an explicit plain-chat path: it streams text and does not expose
	// tools. The full history is still kept and persisted after the response.
	modelMessages := updatedMessages
	if contextManager != nil {
		var buildErr error
		modelMessages, buildErr = contextManager.Build(ctx, updatedMessages, contextx.BuildOptions{
			SessionID: sessionID,
			DebugAPI:  debugAPI,
		})
		if buildErr != nil {
			return llm.Message{}, messages, buildErr
		}
	}
	fmt.Printf("Chat stream: sending %d model messages (%d stored messages)\n", len(modelMessages), len(updatedMessages))
	logEvent(ctx, logger, sessionID, "chat_stream", map[string]any{
		"message_count":       len(updatedMessages),
		"model_message_count": len(modelMessages),
	})

	resp, err := client.GenerateStream(ctx, llm.GenerateRequest{
		Messages: modelMessages,
		Stream:   true,
		DebugAPI: debugAPI,
	}, onDelta)
	if err != nil {
		return llm.Message{}, messages, err
	}

	updatedMessages = append(updatedMessages, resp.Assistant)
	return resp.Assistant, updatedMessages, nil
}

func cliApproveTool(scanner *bufio.Scanner, logger logx.Logger, sessionID string) agent.ApprovalFunc {
	return func(ctx context.Context, req agent.ApprovalRequest) (bool, error) {
		logEvent(ctx, logger, sessionID, "approval_requested", map[string]any{
			"tool":       req.ToolName,
			"permission": string(req.Permission),
			"arguments":  string(req.Arguments),
		})
		fmt.Println()
		fmt.Printf("Tool approval required: %s\n", req.ToolName)
		fmt.Printf("permission: %s\n", req.Permission)
		fmt.Printf("description: %s\n", req.ToolDescription)
		fmt.Printf("arguments: %s\n", compactJSON(req.Arguments))
		fmt.Print("Approve? type yes to continue: ")

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return false, err
			}
			return false, nil
		}

		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		approved := answer == "yes" || answer == "y"
		logEvent(ctx, logger, sessionID, "approval_result", map[string]any{
			"tool":     req.ToolName,
			"approved": approved,
		})
		return approved, nil
	}
}

func logEvent(ctx context.Context, logger logx.Logger, sessionID string, eventType string, data map[string]any) {
	if logger == nil {
		return
	}
	_ = logger.Log(ctx, logx.Event{
		Type:    eventType,
		Session: sessionID,
		Data:    data,
	})
}

func logPlanStep(logger logx.Logger, sessionID string, index int, status plan.StepStatus, text string) {
	logEvent(context.Background(), logger, sessionID, "plan_step", map[string]any{
		"index":  index,
		"status": string(status),
		"text":   text,
	})
}

type logFilter struct {
	Session    string
	All        bool
	Type       string
	ErrorsOnly bool
	MaxLines   int
}

func parseLogCommand(input string, currentSession string) (logFilter, error) {
	filter := logFilter{
		Session:  currentSession,
		MaxLines: defaultLogTail,
	}
	fields := strings.Fields(input)
	if len(fields) == 0 || fields[0] != "/logs" {
		return filter, errors.New("usage: /logs [all|session <id>|type <event>|errors|tail <n>]")
	}
	if len(fields) == 1 {
		return filter, nil
	}

	for i := 1; i < len(fields); i++ {
		switch fields[i] {
		case "all":
			filter.All = true
			filter.Session = ""
		case "current":
			filter.All = false
			filter.Session = currentSession
		case "session":
			if i+1 >= len(fields) {
				return filter, errors.New("usage: /logs session <id>")
			}
			filter.All = false
			filter.Session = fields[i+1]
			i++
		case "type":
			if i+1 >= len(fields) {
				return filter, errors.New("usage: /logs type <event>")
			}
			filter.Type = fields[i+1]
			i++
		case "errors":
			filter.ErrorsOnly = true
		case "tail":
			if i+1 >= len(fields) {
				return filter, errors.New("usage: /logs tail <n>")
			}
			n, err := strconv.Atoi(fields[i+1])
			if err != nil || n <= 0 {
				return filter, errors.New("tail must be a positive integer")
			}
			filter.MaxLines = n
			i++
		default:
			// Shorthand: /logs default means /logs session default.
			if i == 1 && len(fields) == 2 {
				filter.All = false
				filter.Session = fields[i]
				return filter, nil
			}
			return filter, fmt.Errorf("unknown /logs option: %s", fields[i])
		}
	}
	return filter, nil
}

func printRecentLogs(path string, filter logFilter) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Printf("Log file: %s\nNo log events yet.\n", path)
			return nil
		}
		return err
	}

	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		fmt.Printf("Log file: %s\nNo log events yet.\n", path)
		return nil
	}

	var lines []string
	for _, line := range strings.Split(text, "\n") {
		event, ok := parseLogLine(line)
		if !ok || !matchesLogFilter(event, filter) {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		fmt.Printf("Log file: %s\nNo matching log events.\n", path)
		return nil
	}

	start := 0
	if filter.MaxLines > 0 && len(lines) > filter.MaxLines {
		start = len(lines) - filter.MaxLines
	}
	fmt.Printf("Log file: %s\nRecent events:\n", path)
	for _, line := range lines[start:] {
		fmt.Println(line)
	}
	return nil
}

func parseLogLine(line string) (logx.Event, bool) {
	var event logx.Event
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return logx.Event{}, false
	}
	return event, true
}

func matchesLogFilter(event logx.Event, filter logFilter) bool {
	if !filter.All && filter.Session != "" && event.Session != filter.Session {
		return false
	}
	if filter.Type != "" && event.Type != filter.Type {
		return false
	}
	if filter.ErrorsOnly && !isErrorLogEvent(event) {
		return false
	}
	return true
}

func isErrorLogEvent(event logx.Event) bool {
	if value, ok := event.Data["is_error"].(bool); ok && value {
		return true
	}
	if value, ok := event.Data["approved"].(bool); ok && !value {
		return true
	}
	if value, ok := event.Data["status"].(string); ok && value == string(plan.StatusFailed) {
		return true
	}
	return false
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

func printSummary(ctx context.Context, store contextx.SummaryStore, sessionID string) error {
	summary, err := store.Load(ctx, sessionID)
	if err != nil {
		return err
	}
	path, err := store.Path(sessionID)
	if err != nil {
		return err
	}

	fmt.Printf("Summary file: %s\n", path)
	if strings.TrimSpace(summary.Content) == "" {
		fmt.Println("No rolling summary yet.")
		return nil
	}
	fmt.Printf("Summarized non-system messages: %d\n", summary.SummarizedMessages)
	if !summary.UpdatedAt.IsZero() {
		fmt.Printf("Updated at: %s\n", summary.UpdatedAt.Format(time.RFC3339))
	}
	if summary.Model != "" {
		fmt.Printf("Summary model: %s\n", summary.Model)
	}
	fmt.Println()
	fmt.Println(summary.Content)
	return nil
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

func newContextManagerFromEnv(summaryClient llm.Client, summaryStore contextx.SummaryStore, logger logx.Logger, summaryModelName string) (contextx.Manager, error) {
	maxMessages, err := envInt(contextNEnvVar, defaultContextN)
	if err != nil {
		return nil, err
	}
	triggerMessages, err := envInt(summaryTriggerEnvVar, defaultSummaryTrigger)
	if err != nil {
		return nil, err
	}
	keepMessages, err := envInt(summaryKeepEnvVar, defaultSummaryKeep)
	if err != nil {
		return nil, err
	}
	batchMessages, err := envInt(summaryBatchEnvVar, defaultSummaryBatch)
	if err != nil {
		return nil, err
	}
	maxSummaryChars, err := envInt(summaryMaxCharsEnvVar, defaultSummaryMaxChars)
	if err != nil {
		return nil, err
	}
	summaryTimeout, err := envInt(summaryTimeoutEnvVar, defaultSummaryTimeout)
	if err != nil {
		return nil, err
	}

	return contextx.RollingSummaryManager{
		Client:           summaryClient,
		Store:            summaryStore,
		Logger:           logger,
		RecentMessages:   maxMessages,
		TriggerMessages:  triggerMessages,
		KeepMessages:     keepMessages,
		BatchMessages:    batchMessages,
		MaxSummaryChars:  maxSummaryChars,
		TimeoutSeconds:   summaryTimeout,
		SummaryModelName: summaryModelName,
	}, nil
}

func newSummaryClientFromEnv(primary llm.Client) (llm.Client, string, error) {
	model := strings.TrimSpace(os.Getenv(summaryModelEnvVar))
	baseURL := strings.TrimSpace(os.Getenv(summaryBaseURLEnvVar))
	apiKeyEnv := strings.TrimSpace(os.Getenv(summaryAPIKeyEnvVar))
	if model == "" && baseURL == "" && apiKeyEnv == "" {
		return primary, "primary", nil
	}

	apiKey := ""
	if apiKeyEnv != "" {
		apiKey = strings.TrimSpace(os.Getenv(apiKeyEnv))
		if apiKey == "" {
			return nil, "", fmt.Errorf("%s points to empty env var %s", summaryAPIKeyEnvVar, apiKeyEnv)
		}
	} else {
		apiKey = firstNonEmptyEnvValue("DEEPSEEK_API_KEY", "ZAI_API_KEY", "ZHIPUAI_API_KEY", "GLM_API_KEY", "OPENAI_API_KEY", "MINIAGENT_API_KEY")
	}
	if baseURL == "" {
		baseURL = firstNonEmptyEnvValue("DEEPSEEK_BASE_URL", "ZAI_BASE_URL", "ZHIPUAI_BASE_URL", "GLM_BASE_URL", "OPENAI_BASE_URL", "MINIAGENT_BASE_URL")
	}
	if model == "" {
		model = firstNonEmptyEnvValue("DEEPSEEK_MODEL", "ZAI_MODEL", "ZHIPUAI_MODEL", "GLM_MODEL", "OPENAI_MODEL", "MINIAGENT_MODEL")
	}

	enableThinking, reasoningEffort, err := llm.DeepSeekRequestOptionsFromEnv()
	if err != nil {
		return nil, "", err
	}
	client, err := llm.NewOpenAIClient(llm.OpenAIConfig{
		APIKey:          apiKey,
		BaseURL:         baseURL,
		Model:           model,
		EnableThinking:  enableThinking,
		ReasoningEffort: reasoningEffort,
	})
	if err != nil {
		return nil, "", fmt.Errorf("init summary client: %w", err)
	}
	if model == "" {
		model = "default"
	}
	return client, model, nil
}

func envInt(key string, defaultValue int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}

func firstNonEmptyEnvValue(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
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

func printSkills(store skill.Store) error {
	skills, err := store.List()
	if err != nil {
		return err
	}
	if len(skills) == 0 {
		fmt.Printf("No skills found under %s/.\n", store.Root)
		return nil
	}

	fmt.Printf("Skills under %s/:\n", store.Root)
	for _, item := range skills {
		if item.Description == "" {
			fmt.Printf("- %s\n", item.Name)
			continue
		}
		fmt.Printf("- %s: %s\n", item.Name, item.Description)
	}
	return nil
}

func printSkill(store skill.Store, input string) error {
	fields := strings.Fields(input)
	if len(fields) != 2 || fields[0] != "/skill" {
		return errors.New("usage: /skill <name>")
	}

	item, err := store.Load(fields[1])
	if err != nil {
		return err
	}

	fmt.Printf("Skill: %s\n", item.Name)
	if item.Description != "" {
		fmt.Printf("Description: %s\n", item.Description)
	}
	fmt.Printf("Path: %s\n", item.Path)
	fmt.Println()
	fmt.Println(preview(item.Content, 1600))
	if len([]rune(item.Content)) > 1600 {
		fmt.Println("... (preview truncated)")
	}
	return nil
}

func printSkillScripts(store skill.Store, input string) error {
	fields := strings.Fields(input)
	if len(fields) != 2 || fields[0] != "/skill-scripts" {
		return errors.New("usage: /skill-scripts <skill>")
	}

	item, err := store.Load(fields[1])
	if err != nil {
		return err
	}
	scripts, err := store.Scripts(fields[1])
	if err != nil {
		return err
	}

	fmt.Printf("Skill: %s\n", item.Name)
	fmt.Printf("Path: %s\n", item.Path)
	if len(scripts) == 0 {
		fmt.Println("No scripts found.")
		return nil
	}

	fmt.Println("Scripts:")
	for _, script := range scripts {
		fmt.Printf("- %s (%d bytes)\n", script.Path, script.Size)
	}
	fmt.Println("Scripts are listed only. MiniAgent did not execute them.")
	return nil
}

func printSkillManifest(store skill.Store, input string) error {
	fields := strings.Fields(input)
	if len(fields) != 2 || fields[0] != "/skill-manifest" {
		return errors.New("usage: /skill-manifest <skill>")
	}

	item, err := store.Load(fields[1])
	if err != nil {
		return err
	}
	manifest, path, err := store.Manifest(fields[1])
	if err != nil {
		return err
	}

	fmt.Printf("Skill: %s\n", item.Name)
	fmt.Printf("Skill path: %s\n", item.Path)
	fmt.Printf("Manifest: %s\n", path)
	if len(manifest.Scripts) == 0 {
		fmt.Println("No local script permissions found.")
		fmt.Println("No scripts from this skill can be executed by run_skill_script.")
		return nil
	}

	fmt.Println("Script permissions:")
	for _, script := range manifest.Scripts {
		status := "denied"
		if script.Allowed {
			status = "allowed"
		}
		fmt.Printf("- %s [%s]\n", script.Path, status)
		if script.Description != "" {
			fmt.Printf("  description: %s\n", script.Description)
		}
		if script.Reason != "" {
			fmt.Printf("  reason: %s\n", script.Reason)
		}
		runner := script.Runner
		if runner == "" {
			runner = "direct"
		}
		fmt.Printf("  runner: %s\n", runner)
		fmt.Printf("  requires_approval: %t\n", script.RequiresApproval)
		fmt.Printf("  timeout_seconds: %s\n", manifestLimit(script.TimeoutSeconds, "default"))
		fmt.Printf("  max_output_bytes: %s\n", manifestLimit(script.MaxOutputBytes, "default"))
		fmt.Printf("  max_args: %s\n", manifestLimit(script.MaxArgs, "default"))
		fmt.Printf("  max_arg_bytes: %s\n", manifestLimit(script.MaxArgBytes, "default"))
		if len(script.Args) > 0 {
			fmt.Println("  args:")
			for i, arg := range script.Args {
				description := arg.Description
				if description == "" {
					description = "(no description)"
				}
				fmt.Printf("    %d. type=%s description=%s\n", i+1, arg.Type, description)
			}
		}
	}
	fmt.Println("Manifest is local policy only. MiniAgent did not execute any scripts.")
	return nil
}

func manifestLimit(value int, fallback string) string {
	if value <= 0 {
		return fallback
	}
	return strconv.Itoa(value)
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
