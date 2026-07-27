package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"recruitment/llm-gateway-service/internal/config"
)

type OpenAICompatibleProvider struct {
	client         *http.Client
	defaultTimeout time.Duration
}

func NewOpenAICompatibleProvider(defaultTimeout time.Duration) *OpenAICompatibleProvider {
	return &OpenAICompatibleProvider{
		client:         &http.Client{},
		defaultTimeout: defaultTimeout,
	}
}

func (p *OpenAICompatibleProvider) Chat(ctx context.Context, model config.ModelConfig, req ChatRequest) (ChatResponse, error) {
	messages, err := normalizeMessages(req)
	if err != nil {
		return ChatResponse{}, err
	}
	payload := map[string]any{
		"model":       model.Model,
		"messages":    messages,
		"temperature": req.Temperature,
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ChatResponse{}, err
	}
	callCtx, cancel := p.timeoutContext(ctx, model)
	defer cancel()

	start := time.Now()
	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, chatCompletionsURL(model.BaseURL), bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+model.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(httpReq)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return ChatResponse{}, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ChatResponse{}, fmt.Errorf("llm upstream returned %s: %s", resp.Status, truncateForError(string(respBody), 800))
	}
	var parsed chatCompletionsResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return ChatResponse{}, err
	}
	if len(parsed.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("llm upstream returned no choices")
	}
	usage := &Usage{
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		TotalTokens:      parsed.Usage.TotalTokens,
	}
	if usage.TotalTokens == 0 && usage.PromptTokens == 0 && usage.CompletionTokens == 0 {
		usage = nil
	}
	return ChatResponse{
		Model:        model.Name,
		Provider:     model.Provider,
		Content:      parsed.Choices[0].Message.Content,
		FinishReason: parsed.Choices[0].FinishReason,
		LatencyMs:    latency,
		Usage:        usage,
		TraceID:      req.TraceID,
	}, nil
}

func (p *OpenAICompatibleProvider) Stream(ctx context.Context, model config.ModelConfig, req ChatRequest, onChunk func(StreamChunk) error) (ChatResponse, error) {
	messages, err := normalizeMessages(req)
	if err != nil {
		return ChatResponse{}, err
	}
	payload := map[string]any{
		"model":       model.Model,
		"messages":    messages,
		"temperature": req.Temperature,
		"stream":      true,
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ChatResponse{}, err
	}
	callCtx, cancel := p.timeoutContext(ctx, model)
	defer cancel()

	start := time.Now()
	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, chatCompletionsURL(model.BaseURL), bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+model.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
		if readErr != nil {
			return ChatResponse{}, readErr
		}
		return ChatResponse{}, fmt.Errorf("llm upstream returned %s: %s", resp.Status, truncateForError(string(respBody), 800))
	}

	var content strings.Builder
	var finishReason string
	var usage *Usage
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var event chatCompletionsStreamResponse
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return ChatResponse{}, err
		}
		if event.Usage.TotalTokens > 0 || event.Usage.PromptTokens > 0 || event.Usage.CompletionTokens > 0 {
			usage = &Usage{
				PromptTokens:     event.Usage.PromptTokens,
				CompletionTokens: event.Usage.CompletionTokens,
				TotalTokens:      event.Usage.TotalTokens,
			}
		}
		for _, choice := range event.Choices {
			if strings.TrimSpace(choice.FinishReason) != "" {
				finishReason = choice.FinishReason
			}
			if choice.Delta.Content == "" {
				continue
			}
			content.WriteString(choice.Delta.Content)
			if onChunk != nil {
				if err := onChunk(StreamChunk{
					Content:  choice.Delta.Content,
					Model:    model.Name,
					Provider: model.Provider,
					TraceID:  req.TraceID,
				}); err != nil {
					return ChatResponse{}, err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return ChatResponse{}, err
	}
	return ChatResponse{
		Model:        model.Name,
		Provider:     model.Provider,
		Content:      content.String(),
		FinishReason: finishReason,
		LatencyMs:    time.Since(start).Milliseconds(),
		Usage:        usage,
		TraceID:      req.TraceID,
	}, nil
}

func (p *OpenAICompatibleProvider) timeoutContext(ctx context.Context, model config.ModelConfig) (context.Context, context.CancelFunc) {
	timeout := time.Duration(model.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = p.defaultTimeout
	}
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	return context.WithTimeout(ctx, timeout)
}

func normalizeMessages(req ChatRequest) ([]map[string]string, error) {
	messages := make([]map[string]string, 0, len(req.Messages)+2)
	if systemPrompt := strings.TrimSpace(req.SystemPrompt); systemPrompt != "" {
		messages = append(messages, map[string]string{"role": "system", "content": systemPrompt})
	}
	for _, msg := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if role == "" {
			role = "user"
		}
		if role != "system" && role != "user" && role != "assistant" {
			return nil, fmt.Errorf("unsupported message role %q", msg.Role)
		}
		messages = append(messages, map[string]string{"role": role, "content": content})
	}
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		messages = append(messages, map[string]string{"role": "user", "content": prompt})
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("prompt or messages is required")
	}
	return messages, nil
}

func chatCompletionsURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/chat/completions") {
		return baseURL
	}
	return baseURL + "/chat/completions"
}

func truncateForError(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

type chatCompletionsResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int32 `json:"prompt_tokens"`
		CompletionTokens int32 `json:"completion_tokens"`
		TotalTokens      int32 `json:"total_tokens"`
	} `json:"usage"`
}

type chatCompletionsStreamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int32 `json:"prompt_tokens"`
		CompletionTokens int32 `json:"completion_tokens"`
		TotalTokens      int32 `json:"total_tokens"`
	} `json:"usage"`
}
