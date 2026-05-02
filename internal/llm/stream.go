package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

func (c *OpenAIClient) GenerateStream(ctx context.Context, req GenerateRequest, onDelta func(string)) (GenerateResponse, error) {
	body, err := json.Marshal(c.newChatRequest(req, true))
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("marshal request: %w", err)
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

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return GenerateResponse{}, fmt.Errorf("read error response: %w", readErr)
		}
		return GenerateResponse{}, fmt.Errorf("chat completion failed: %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	assistant, err := readSSEStream(resp.Body, onDelta)
	if err != nil {
		return GenerateResponse{}, err
	}

	return GenerateResponse{Assistant: assistant}, nil
}

func readSSEStream(body io.Reader, onDelta func(string)) (Message, error) {
	reader := bufio.NewReader(body)
	var fullText strings.Builder
	var eventData []string

	flushEvent := func() (bool, error) {
		if len(eventData) == 0 {
			return false, nil
		}

		payload := strings.Join(eventData, "\n")
		eventData = eventData[:0]

		if payload == "[DONE]" {
			return true, nil
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return false, fmt.Errorf("decode stream chunk: %w", err)
		}

		for _, choice := range chunk.Choices {
			delta := choice.Delta.Content
			if delta == "" {
				delta = choice.Message.Content
			}
			if delta == "" {
				continue
			}

			fullText.WriteString(delta)
			if onDelta != nil {
				onDelta(delta)
			}
		}

		return false, nil
	}

	for {
		line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			done, flushErr := flushEvent()
			if flushErr != nil {
				return Message{}, flushErr
			}
			if done || fullText.Len() > 0 {
				break
			}
			return Message{}, errors.New("stream ended before any content was received")
		}
		if err != nil {
			return Message{}, fmt.Errorf("read stream: %w", err)
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			done, flushErr := flushEvent()
			if flushErr != nil {
				return Message{}, flushErr
			}
			if done {
				break
			}
			continue
		}

		if strings.HasPrefix(line, "data:") {
			eventData = append(eventData, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	return Message{
		Role:    RoleAssistant,
		Content: fullText.String(),
	}, nil
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta   openAIChatDelta   `json:"delta"`
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
}

type openAIChatDelta struct {
	Content string `json:"content"`
}
