package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://open.bigmodel.cn/api/paas/v4"
	defaultModel   = "glm-4.6v"
)

type OpenAIConfig struct {
	APIKey          string
	BaseURL         string
	Model           string
	EnableThinking  *bool
	ReasoningEffort string
	Client          *http.Client
}

type OpenAIClient struct {
	apiKey          string
	baseURL         string
	model           string
	enableThinking  *bool
	reasoningEffort string
	httpClient      *http.Client
}

func NewOpenAIClient(cfg OpenAIConfig) (*OpenAIClient, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("missing API key")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultModel
	}

	httpClient := cfg.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	return &OpenAIClient{
		apiKey:          cfg.APIKey,
		baseURL:         baseURL,
		model:           model,
		enableThinking:  cfg.EnableThinking,
		reasoningEffort: strings.TrimSpace(cfg.ReasoningEffort),
		httpClient:      httpClient,
	}, nil
}

func NewOpenAIClientFromEnv() (*OpenAIClient, error) {
	apiKey := firstNonEmptyEnv(
		"DEEPSEEK_API_KEY",
		"ZAI_API_KEY",
		"ZHIPUAI_API_KEY",
		"GLM_API_KEY",
		"OPENAI_API_KEY",
		"MINIAGENT_API_KEY",
	)

	baseURL := firstNonEmptyEnv(
		"DEEPSEEK_BASE_URL",
		"ZAI_BASE_URL",
		"ZHIPUAI_BASE_URL",
		"GLM_BASE_URL",
		"OPENAI_BASE_URL",
		"MINIAGENT_BASE_URL",
	)

	model := firstNonEmptyEnv(
		"DEEPSEEK_MODEL",
		"ZAI_MODEL",
		"ZHIPUAI_MODEL",
		"GLM_MODEL",
		"OPENAI_MODEL",
		"MINIAGENT_MODEL",
	)
	enableThinking, reasoningEffort, err := DeepSeekRequestOptionsFromEnv()
	if err != nil {
		return nil, err
	}

	return NewOpenAIClient(OpenAIConfig{
		APIKey:          apiKey,
		BaseURL:         baseURL,
		Model:           model,
		EnableThinking:  enableThinking,
		ReasoningEffort: reasoningEffort,
	})
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func DeepSeekRequestOptionsFromEnv() (*bool, string, error) {
	if !hasAnyEnv("DEEPSEEK_API_KEY", "DEEPSEEK_BASE_URL", "DEEPSEEK_MODEL") {
		return nil, "", nil
	}

	enableThinking, err := optionalBoolEnv("DEEPSEEK_ENABLE_THINKING")
	if err != nil {
		return nil, "", err
	}
	return enableThinking, firstNonEmptyEnv("DEEPSEEK_REASONING_EFFORT"), nil
}

func optionalBoolEnv(key string) (*bool, error) {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return nil, nil
	}
	switch value {
	case "1", "true", "yes", "on":
		enabled := true
		return &enabled, nil
	case "0", "false", "no", "off":
		enabled := false
		return &enabled, nil
	default:
		return nil, fmt.Errorf("invalid %s: use true or false", key)
	}
}

func hasAnyEnv(keys ...string) bool {
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

func (c *OpenAIClient) Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	body, err := json.Marshal(c.newChatRequest(req, false))
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("marshal request: %w", err)
	}
	if req.DebugAPI {
		debugJSON("API request body", body)
	}

	httpReq, err := c.newHTTPRequest(ctx, body)
	if err != nil {
		return GenerateResponse{}, err
	}
	resp, err := c.do(httpReq)
	if err != nil {
		return GenerateResponse{}, err
	}

	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("read response: %w", err)
	}
	if req.DebugAPI {
		debugJSON("API response body", respBody)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GenerateResponse{}, fmt.Errorf("chat completion failed: %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	var chatResp openAIChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return GenerateResponse{}, fmt.Errorf("decode response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return GenerateResponse{}, errors.New("empty choices in response")
	}

	msg := chatResp.Choices[0].Message
	toolCalls := fromOpenAIToolCalls(msg.ToolCalls)

	assistant := Message{
		Role:      RoleAssistant,
		Content:   msg.Content,
		ToolCalls: toolCalls,
	}

	return GenerateResponse{
		Assistant: assistant,
		ToolCalls: toolCalls,
	}, nil
}

// openAITool is the OpenAI-compatible function-tool shape sent in the HTTP
// request. It is the model-facing description of a local Go tool.
type openAITool struct {
	Type     string            `json:"type"`
	Function openAIFunctionDef `json:"function"`
}

type openAIFunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatRequest struct {
	Model           string              `json:"model"`
	Messages        []openAIChatMessage `json:"messages"`
	Tools           []openAITool        `json:"tools,omitempty"`
	Stream          bool                `json:"stream,omitempty"`
	EnableThinking  *bool               `json:"enable_thinking,omitempty"`
	ReasoningEffort string              `json:"reasoning_effort,omitempty"`
}

type openAIChatMessage struct {
	Role       Role             `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
}

func toOpenAITools(tools []ToolSchema) []openAITool {
	out := make([]openAITool, 0, len(tools))
	for _, tool := range tools {
		// ToolSchema is our provider-neutral shape; openAITool is the
		// OpenAI-compatible wire format that the model actually receives.
		out = append(out, openAITool{
			Type: "function",
			Function: openAIFunctionDef{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}
	return out
}

func toOpenAIToolCalls(calls []ToolCall) []openAIToolCall {
	out := make([]openAIToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, openAIToolCall{
			ID:   call.ID,
			Type: "function",
			Function: openAIFunctionCall{
				Name:      call.Name,
				Arguments: string(call.Arguments),
			},
		})
	}
	return out
}

func toOpenAIMessages(messages []Message) []openAIChatMessage {
	out := make([]openAIChatMessage, 0, len(messages))
	for _, message := range messages {
		// Tool result messages carry ToolCallID so the model can connect a
		// result back to the assistant tool call that requested it.
		out = append(out, openAIChatMessage{
			Role:       message.Role,
			Content:    message.Content,
			ToolCalls:  toOpenAIToolCalls(message.ToolCalls),
			ToolCallID: message.ToolCallID,
			Name:       message.ToolName,
		})
	}
	return out
}

func (c *OpenAIClient) newChatRequest(req GenerateRequest, stream bool) openAIChatRequest {
	return openAIChatRequest{
		Model:           c.model,
		Messages:        toOpenAIMessages(req.Messages),
		Tools:           toOpenAITools(req.Tools),
		Stream:          stream,
		EnableThinking:  c.enableThinking,
		ReasoningEffort: c.reasoningEffort,
	}
}

func (c *OpenAIClient) newHTTPRequest(ctx context.Context, body []byte) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	return httpReq, nil
}

func (c *OpenAIClient) do(req *http.Request) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	return resp, nil
}

func fromOpenAIToolCalls(calls []openAIToolCall) []ToolCall {
	out := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		args := call.Function.Arguments
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}

		out = append(out, ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: json.RawMessage(args),
		})
	}
	return out
}

func debugJSON(label string, data []byte) {
	fmt.Fprintf(os.Stderr, "\n--- %s raw ---\n%s\n", label, data)

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, data, "", "  "); err != nil {
		fmt.Fprintf(os.Stderr, "--- %s pretty ---\n<invalid json: %v>\n", label, err)
		return
	}

	fmt.Fprintf(os.Stderr, "--- %s pretty ---\n%s\n--- end %s ---\n\n", label, pretty.String(), label)
}
