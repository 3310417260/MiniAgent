package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"miniagent/internal/llm"
	"miniagent/internal/skill"
)

const (
	defaultSkillScriptTimeoutSeconds = 5
	maxSkillScriptTimeoutSeconds     = 15
	defaultSkillScriptMaxOutputBytes = 4000
	maxSkillScriptOutputBytes        = 12000
	maxSkillScriptArgs               = 12
	maxSkillScriptArgBytes           = 200
	skillScriptRunnerDirect          = "direct"
	skillScriptRunnerPython3         = "python3"
)

type RunSkillScriptTool struct {
	Store skill.Store
}

type skillScriptRequest struct {
	Skill          string   `json:"skill"`
	Script         string   `json:"script"`
	Args           []string `json:"args"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	MaxOutputBytes int      `json:"max_output_bytes"`
}

type preparedSkillScript struct {
	Skill          string
	Script         string
	Runner         string
	Root           string
	ScriptAbs      string
	Rel            string
	Args           []string
	TimeoutSeconds int
	MaxOutputBytes int
}

type allowedSkillScript struct {
	Skill  string
	Script skill.ScriptPermission
}

func (RunSkillScriptTool) Name() string {
	return "run_skill_script"
}

func (RunSkillScriptTool) Description() string {
	return "Run a small allowlisted script packaged with a local skill."
}

func (RunSkillScriptTool) Permission() Permission {
	return PermissionShell
}

func (t RunSkillScriptTool) Schema() llm.ToolSchema {
	skills, scripts := t.allowedSchemaEnums()
	skillProperty := map[string]any{
		"type":        "string",
		"description": "Skill folder name. The skill/script pair must be allowed by skills/<skill>/manifest.local.json.",
	}
	if len(skills) > 0 {
		skillProperty["enum"] = skills
	}
	scriptProperty := map[string]any{
		"type":        "string",
		"description": "Script path relative to the skill folder. The script must be allowed by manifest.local.json.",
	}
	if len(scripts) > 0 {
		scriptProperty["enum"] = scripts
	}

	return llm.ToolSchema{
		Name:        "run_skill_script",
		Description: "Run a script under skills/<skill>/scripts only when allowed by skills/<skill>/manifest.local.json and explicitly approved. The script runs directly, not through a shell.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"skill":  skillProperty,
				"script": scriptProperty,
				"args": map[string]any{
					"type":        "array",
					"description": "Script arguments as separate array items. Do not pass shell strings.",
					"items": map[string]any{
						"type": "string",
					},
				},
				"timeout_seconds": map[string]any{
					"type":        "integer",
					"description": "Execution timeout in seconds. Defaults to 5 and is capped at 15.",
				},
				"max_output_bytes": map[string]any{
					"type":        "integer",
					"description": "Maximum stdout/stderr bytes to return. Defaults to 4000 and is capped at 12000.",
				},
			},
			"required": []string{"skill", "script", "args"},
		},
	}
}

func (t RunSkillScriptTool) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	prepared, invalid, err := t.prepareSkillScript(input)
	if err != nil || invalid.IsError {
		return invalid, err
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(prepared.TimeoutSeconds)*time.Second)
	defer cancel()

	command, commandArgs := prepared.command()
	// Run the script directly, or through an allowlisted runner from manifest.
	// We never pass through `sh -c`, so arguments remain data and cannot become
	// a composed shell command.
	cmd := exec.CommandContext(runCtx, command, commandArgs...)
	cmd.Dir = prepared.Root

	stdout := &limitedBuffer{limit: prepared.MaxOutputBytes}
	stderr := &limitedBuffer{limit: prepared.MaxOutputBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err = cmd.Run()
	exitCode := 0
	timedOut := false
	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			timedOut = true
			exitCode = -1
		} else {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				return ErrorResult(ToolError{
					Type:              ErrorExecution,
					Message:           "run skill script: " + err.Error(),
					Recoverable:       true,
					SuggestedNextStep: "Check the script runner and manifest, then retry only if the script is executable.",
					Details: map[string]any{
						"skill":  prepared.Skill,
						"script": prepared.Script,
						"runner": prepared.Runner,
					},
				}), nil
			}
		}
	}

	lines := []string{
		"skill_script: " + prepared.Rel,
		"args: " + strings.Join(prepared.Args, " "),
		fmt.Sprintf("exit_code: %d", exitCode),
	}
	if timedOut {
		lines = append(lines, fmt.Sprintf("timed_out: true after %ds", prepared.TimeoutSeconds))
	}
	lines = appendShellOutput(lines, "stdout", stdout)
	lines = appendShellOutput(lines, "stderr", stderr)

	event := Event{
		Type: "skill_script_execute",
		Data: map[string]any{
			"skill":            prepared.Skill,
			"script":           prepared.Script,
			"runner":           prepared.Runner,
			"exit_code":        exitCode,
			"timed_out":        timedOut,
			"timeout_seconds":  prepared.TimeoutSeconds,
			"max_output_bytes": prepared.MaxOutputBytes,
			"stdout_truncated": stdout.truncated,
			"stderr_truncated": stderr.truncated,
		},
	}
	content := strings.Join(lines, "\n")
	if exitCode != 0 {
		errType := ErrorExecution
		nextStep := "Read stdout/stderr, adjust script arguments if possible, and retry only if the failure is fixable."
		if timedOut {
			errType = ErrorTimeout
			nextStep = "Use smaller input or a shorter-running script invocation before retrying."
		}
		return ErrorResult(ToolError{
			Type:              errType,
			Message:           content,
			Recoverable:       true,
			SuggestedNextStep: nextStep,
			Details: map[string]any{
				"skill":     prepared.Skill,
				"script":    prepared.Script,
				"runner":    prepared.Runner,
				"exit_code": exitCode,
				"timed_out": timedOut,
			},
		}, event), nil
	}

	return Result{
		Content: content,
		Events:  []Event{event},
	}, nil
}

func (t RunSkillScriptTool) Preflight(ctx context.Context, input json.RawMessage) (Result, error) {
	prepared, invalid, err := t.prepareSkillScript(input)
	if err != nil || invalid.IsError {
		return invalid, err
	}
	return Result{
		Events: []Event{
			skillScriptPreflightEvent(prepared.Skill, prepared.Script, true, ""),
		},
	}, nil
}

func (t RunSkillScriptTool) prepareSkillScript(input json.RawMessage) (preparedSkillScript, Result, error) {
	var args skillScriptRequest
	if err := json.Unmarshal(defaultJSON(input), &args); err != nil {
		return preparedSkillScript{}, skillScriptPreflightError("", "", "invalid_arguments", "invalid arguments: "+err.Error()), nil
	}

	skillName := strings.TrimSpace(args.Skill)
	scriptPath := filepath.ToSlash(strings.TrimSpace(args.Script))
	if skillName == "" || scriptPath == "" {
		return preparedSkillScript{}, skillScriptPreflightError(skillName, scriptPath, "missing_skill_or_script", "skill and script are required"), nil
	}
	policy, ok, err := t.allowedScript(skillName, scriptPath)
	if err != nil {
		return preparedSkillScript{}, Result{}, err
	}
	if !ok {
		return preparedSkillScript{}, skillScriptPreflightError(skillName, scriptPath, "not_allowlisted", "skill script is not allowlisted: "+skillName+"/"+scriptPath), nil
	}
	if err := validateSkillScriptArgs(args.Args, policy); err != nil {
		return preparedSkillScript{}, skillScriptPreflightError(skillName, scriptPath, "invalid_args", err.Error()), nil
	}
	runner, err := normalizeSkillScriptRunner(policy.Runner)
	if err != nil {
		return preparedSkillScript{}, skillScriptPreflightError(skillName, scriptPath, "invalid_runner", err.Error()), nil
	}

	root, scriptAbs, rel, err := skillScriptPath(skillName, scriptPath)
	if err != nil {
		return preparedSkillScript{}, skillScriptPreflightError(skillName, scriptPath, "invalid_path", err.Error()), nil
	}
	info, err := os.Stat(scriptAbs)
	if err != nil {
		return preparedSkillScript{}, skillScriptPreflightError(skillName, scriptPath, "stat_failed", "stat skill script: "+err.Error()), nil
	}
	if info.IsDir() {
		return preparedSkillScript{}, skillScriptPreflightError(skillName, scriptPath, "script_is_directory", "skill script is a directory: "+rel), nil
	}

	return preparedSkillScript{
		Skill:          skillName,
		Script:         scriptPath,
		Runner:         runner,
		Root:           root,
		ScriptAbs:      scriptAbs,
		Rel:            rel,
		Args:           args.Args,
		TimeoutSeconds: boundedSkillScriptTimeout(args.TimeoutSeconds, policy.TimeoutSeconds),
		MaxOutputBytes: boundedSkillScriptOutput(args.MaxOutputBytes, policy.MaxOutputBytes),
	}, Result{}, nil
}

func (p preparedSkillScript) command() (string, []string) {
	switch p.Runner {
	case skillScriptRunnerPython3:
		return "python3", append([]string{p.ScriptAbs}, p.Args...)
	default:
		return p.ScriptAbs, p.Args
	}
}

func skillScriptPreflightError(skillName string, scriptPath string, reason string, content string) Result {
	return ErrorResult(skillScriptToolError(skillName, scriptPath, reason, content), skillScriptPreflightEvent(skillName, scriptPath, false, reason))
}

func skillScriptToolError(skillName string, scriptPath string, reason string, content string) ToolError {
	errType := ErrorValidation
	recoverable := true
	nextStep := "Correct the skill/script arguments and retry if appropriate."
	switch reason {
	case "not_allowlisted":
		errType = ErrorScriptNotAllowed
		nextStep = "Use /skill-manifest to inspect allowed scripts, then choose an allowlisted script or explain that this script is not permitted."
	case "invalid_path":
		errType = ErrorPathNotAllowed
		recoverable = false
		nextStep = "Use a script path inside skills/<skill>/scripts."
	case "stat_failed":
		errType = ErrorNotFound
		nextStep = "Use /skill-scripts to inspect available script files, then retry with an existing script."
	case "script_is_directory":
		errType = ErrorValidation
		nextStep = "Retry with a script file, not a directory."
	}
	return ToolError{
		Type:              errType,
		Message:           content,
		Recoverable:       recoverable,
		SuggestedNextStep: nextStep,
		Details: map[string]any{
			"skill":  skillName,
			"script": scriptPath,
			"reason": reason,
		},
	}
}

func skillScriptPreflightEvent(skillName string, scriptPath string, allowed bool, reason string) Event {
	data := map[string]any{
		"skill":   skillName,
		"script":  scriptPath,
		"allowed": allowed,
	}
	if reason != "" {
		data["reason"] = reason
	}
	return Event{
		Type: "skill_script_preflight",
		Data: data,
	}
}

func (t RunSkillScriptTool) allowedScript(skillName string, scriptPath string) (skill.ScriptPermission, bool, error) {
	manifest, _, err := t.store().Manifest(skillName)
	if err != nil {
		return skill.ScriptPermission{}, false, err
	}
	for _, script := range manifest.Scripts {
		if script.Path == scriptPath && script.Allowed {
			return script, true, nil
		}
	}
	return skill.ScriptPermission{}, false, nil
}

func (t RunSkillScriptTool) allowedSchemaEnums() ([]string, []string) {
	allowed, err := t.allowedScripts()
	if err != nil {
		return nil, nil
	}

	skillSeen := map[string]bool{}
	scriptSeen := map[string]bool{}
	var skills []string
	var scripts []string
	for _, item := range allowed {
		if !skillSeen[item.Skill] {
			skillSeen[item.Skill] = true
			skills = append(skills, item.Skill)
		}
		if !scriptSeen[item.Script.Path] {
			scriptSeen[item.Script.Path] = true
			scripts = append(scripts, item.Script.Path)
		}
	}
	return skills, scripts
}

func (t RunSkillScriptTool) allowedScripts() ([]allowedSkillScript, error) {
	catalog, err := t.store().Catalog()
	if err != nil {
		return nil, err
	}

	var allowed []allowedSkillScript
	for _, entry := range catalog {
		manifest, _, err := t.store().Manifest(entry.Name)
		if err != nil {
			return nil, err
		}
		for _, script := range manifest.Scripts {
			if script.Allowed {
				allowed = append(allowed, allowedSkillScript{
					Skill:  entry.Name,
					Script: script,
				})
			}
		}
	}
	return allowed, nil
}

func (t RunSkillScriptTool) store() skill.Store {
	if strings.TrimSpace(t.Store.Root) == "" {
		return skill.NewStore("skills")
	}
	return t.Store
}

func validateSkillScriptArgs(args []string, policy skill.ScriptPermission) error {
	maxArgs := policy.MaxArgs
	if maxArgs <= 0 || maxArgs > maxSkillScriptArgs {
		maxArgs = maxSkillScriptArgs
	}
	maxArgBytes := policy.MaxArgBytes
	if maxArgBytes <= 0 || maxArgBytes > maxSkillScriptArgBytes {
		maxArgBytes = maxSkillScriptArgBytes
	}

	if len(args) > maxArgs {
		return fmt.Errorf("too many script arguments: max %d", maxArgs)
	}
	if len(policy.Args) > 0 && len(args) != len(policy.Args) {
		return fmt.Errorf("script expects %d arguments, got %d", len(policy.Args), len(args))
	}
	for _, arg := range args {
		if len([]byte(arg)) > maxArgBytes {
			return fmt.Errorf("script argument is too long: max %d bytes", maxArgBytes)
		}
		if strings.ContainsAny(arg, "\x00\r\n") {
			return errors.New("script arguments must not contain null bytes or newlines")
		}
	}
	for i, argPolicy := range policy.Args {
		if err := validateSkillScriptArgType(args[i], argPolicy.Type); err != nil {
			return fmt.Errorf("argument %d: %w", i+1, err)
		}
	}
	return nil
}

func validateSkillScriptArgType(value string, argType string) error {
	switch strings.TrimSpace(argType) {
	case "", "string":
		return nil
	case "integer":
		if _, err := strconv.Atoi(value); err != nil {
			return errors.New("must be an integer")
		}
		return nil
	default:
		return fmt.Errorf("unsupported manifest argument type %q", argType)
	}
}

func normalizeSkillScriptRunner(runner string) (string, error) {
	switch strings.TrimSpace(runner) {
	case "", skillScriptRunnerDirect:
		return skillScriptRunnerDirect, nil
	case skillScriptRunnerPython3:
		return skillScriptRunnerPython3, nil
	default:
		return "", fmt.Errorf("unsupported skill script runner %q", runner)
	}
}

func boundedSkillScriptTimeout(requested int, manifestLimit int) int {
	limit := clamp(manifestLimit, defaultSkillScriptTimeoutSeconds, maxSkillScriptTimeoutSeconds)
	if requested <= 0 || requested > limit {
		return limit
	}
	return requested
}

func boundedSkillScriptOutput(requested int, manifestLimit int) int {
	limit := clamp(manifestLimit, defaultSkillScriptMaxOutputBytes, maxSkillScriptOutputBytes)
	if requested <= 0 || requested > limit {
		return limit
	}
	return requested
}

func skillScriptPath(skillName string, scriptPath string) (string, string, string, error) {
	if skillName == "" || strings.Contains(skillName, "/") || strings.Contains(skillName, `\`) || strings.Contains(skillName, "..") {
		return "", "", "", fmt.Errorf("invalid skill name: %q", skillName)
	}
	if filepath.IsAbs(scriptPath) || strings.Contains(scriptPath, `\`) || strings.Contains(scriptPath, "..") {
		return "", "", "", fmt.Errorf("invalid skill script path: %q", scriptPath)
	}

	root, err := filepath.Abs(workspaceRoot())
	if err != nil {
		return "", "", "", err
	}
	scriptsRoot := filepath.Join(root, "skills", skillName, "scripts")
	target := filepath.Join(root, "skills", skillName, filepath.FromSlash(scriptPath))
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", "", "", err
	}

	relToScripts, err := filepath.Rel(scriptsRoot, targetAbs)
	if err != nil {
		return "", "", "", err
	}
	if relToScripts == ".." || strings.HasPrefix(relToScripts, ".."+string(filepath.Separator)) {
		return "", "", "", fmt.Errorf("skill script escapes scripts directory: %s", scriptPath)
	}

	relToWorkspace, err := filepath.Rel(root, targetAbs)
	if err != nil {
		return "", "", "", err
	}
	return root, targetAbs, filepath.ToSlash(relToWorkspace), nil
}
