package tools

import (
	"encoding/json"
	"fmt"
)

type ErrorType string

const (
	ErrorValidation        ErrorType = "validation_error"
	ErrorPermissionDenied  ErrorType = "permission_denied"
	ErrorNotFound          ErrorType = "not_found"
	ErrorNotUnique         ErrorType = "not_unique"
	ErrorPathNotAllowed    ErrorType = "path_not_allowed"
	ErrorCommandNotAllowed ErrorType = "command_not_allowed"
	ErrorScriptNotAllowed  ErrorType = "script_not_allowed"
	ErrorTimeout           ErrorType = "timeout"
	ErrorExecution         ErrorType = "execution_error"
	ErrorUnknownTool       ErrorType = "unknown_tool"
	ErrorUnknown           ErrorType = "unknown_error"
)

type ToolError struct {
	Type              ErrorType      `json:"error_type"`
	Message           string         `json:"message"`
	Recoverable       bool           `json:"recoverable"`
	SuggestedNextStep string         `json:"suggested_next_step,omitempty"`
	Details           map[string]any `json:"details,omitempty"`
}

func (e ToolError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return string(e.Type)
}

func ErrorResult(toolErr ToolError, events ...Event) Result {
	if toolErr.Type == "" {
		toolErr.Type = ErrorUnknown
	}
	payload := map[string]any{
		"ok":          false,
		"error_type":  toolErr.Type,
		"message":     toolErr.Error(),
		"recoverable": toolErr.Recoverable,
	}
	if toolErr.SuggestedNextStep != "" {
		payload["suggested_next_step"] = toolErr.SuggestedNextStep
	}
	if len(toolErr.Details) > 0 {
		payload["details"] = toolErr.Details
	}

	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(fmt.Sprintf(`{"ok":false,"error_type":"%s","message":%q,"recoverable":%t}`, toolErr.Type, toolErr.Error(), toolErr.Recoverable))
	}
	return Result{
		Content: string(data),
		IsError: true,
		Error:   &toolErr,
		Events:  events,
	}
}

func EnsureErrorResult(result Result, fallback ToolError) Result {
	if !result.IsError || result.Error != nil {
		return result
	}
	if fallback.Message == "" {
		fallback.Message = result.Content
	}
	result.Content = ErrorResult(fallback).Content
	result.Error = &fallback
	return result
}

func ExecutionToolError(message string) ToolError {
	return ToolError{
		Type:        ErrorExecution,
		Message:     message,
		Recoverable: true,
	}
}
