package ai

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
)

const (
	maxLLMEvaluationModels = 8

	LLMTaskSimpleJDQuery        = "simple_jd_query"
	LLMTaskHRSearchPlan         = "hr_search_plan"
	LLMTaskRecommendationResult = "resume_recommendation_result"
)

type LLMModel struct {
	Name       string
	Provider   string
	BaseURL    string
	Model      string
	Configured bool
}

type LLMMessage struct {
	Role    string
	Content string
}

type LLMChatInput struct {
	Task         string
	Model        string
	SystemPrompt string
	Prompt       string
	Messages     []LLMMessage
	Temperature  float32
	MaxTokens    int32
}

type LLMUsage struct {
	PromptTokens     int32
	CompletionTokens int32
	TotalTokens      int32
}

type LLMChatOutput struct {
	Model        string
	Provider     string
	Content      string
	FinishReason string
	LatencyMs    int64
	Usage        LLMUsage
	CostUSD      float64
	TraceID      string
	FallbackFrom string
}

type LLMEvaluationResult struct {
	Model        string
	Provider     string
	Content      string
	FinishReason string
	LatencyMs    int64
	Usage        LLMUsage
	CostUSD      float64
	Error        string
}

type LLMStreamChunk struct {
	Content      string
	FinishReason string
	Usage        LLMUsage
}

func (c *Client) ListLLMModels() []LLMModel {
	var resp llmModelsResponse
	if err := c.llmGatewayJSON(context.Background(), http.MethodGet, "/api/v1/models", nil, &resp); err != nil {
		return nil
	}
	models := make([]LLMModel, 0, len(resp.Models))
	for _, model := range resp.Models {
		models = append(models, LLMModel{
			Name:       model.Name,
			Provider:   model.Provider,
			BaseURL:    model.BaseURL,
			Model:      model.Model,
			Configured: model.Configured,
		})
	}
	return models
}

func (c *Client) ChatLLM(ctx context.Context, input LLMChatInput) (LLMChatOutput, error) {
	req := llmChatRequest(input)
	var resp llmChatResponse
	if err := c.llmGatewayJSON(ctx, http.MethodPost, "/api/v1/chat", req, &resp); err != nil {
		return LLMChatOutput{}, err
	}
	return llmChatOutput(resp), nil
}

func (c *Client) StreamLLM(ctx context.Context, input LLMChatInput, onChunk func(LLMStreamChunk) error) (LLMChatOutput, error) {
	req := llmChatRequest(input)
	req.Task = input.Task
	body, err := json.Marshal(req)
	if err != nil {
		return LLMChatOutput{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.llmGatewayURL("/api/v1/stream"), bytes.NewReader(body))
	if err != nil {
		return LLMChatOutput{}, err
	}
	c.decorateLLMGatewayRequest(httpReq)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return LLMChatOutput{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		if readErr != nil {
			return LLMChatOutput{}, readErr
		}
		return LLMChatOutput{}, fmt.Errorf("llm gateway returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	var out LLMChatOutput
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var chunk llmStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return LLMChatOutput{}, err
		}
		out.Model = chunk.Model
		out.Provider = chunk.Provider
		out.TraceID = chunk.TraceID
		out.FinishReason = chunk.FinishReason
		if chunk.Error != "" {
			return LLMChatOutput{}, fmt.Errorf("%s", chunk.Error)
		}
		if chunk.Content != "" {
			out.Content += chunk.Content
			if onChunk != nil {
				if err := onChunk(LLMStreamChunk{Content: chunk.Content}); err != nil {
					return LLMChatOutput{}, err
				}
			}
		}
		if chunk.Done {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return LLMChatOutput{}, err
	}
	return out, nil
}

func (c *Client) EvaluateLLM(ctx context.Context, input LLMChatInput, modelNames []string) ([]LLMEvaluationResult, error) {
	if len(modelNames) > maxLLMEvaluationModels {
		return nil, fmt.Errorf("models cannot exceed %d", maxLLMEvaluationModels)
	}
	req := llmEvaluateRequest{
		Task:         input.Task,
		Models:       modelNames,
		SystemPrompt: input.SystemPrompt,
		Prompt:       input.Prompt,
		Messages:     llmMessages(input.Messages),
		Temperature:  input.Temperature,
		MaxTokens:    input.MaxTokens,
	}
	var resp llmEvaluateResponse
	if err := c.llmGatewayJSON(ctx, http.MethodPost, "/api/v1/evaluate", req, &resp); err != nil {
		return nil, err
	}
	results := make([]LLMEvaluationResult, 0, len(resp.Results))
	for _, result := range resp.Results {
		results = append(results, llmEvaluationResult(result))
	}
	return results, nil
}

func (c *Client) llmGatewayJSON(ctx context.Context, method string, path string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	timeout := time.Duration(c.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.llmGatewayURL(path), body)
	if err != nil {
		return err
	}
	c.decorateLLMGatewayRequest(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("llm gateway returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func (c *Client) llmGatewayURL(path string) string {
	base := strings.TrimRight(strings.TrimSpace(c.cfg.Gateway.Address), "/")
	if base == "" {
		base = "http://127.0.0.1:8090"
	}
	return base + path
}

func (c *Client) decorateLLMGatewayRequest(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(c.cfg.Gateway.Token); token != "" {
		req.Header.Set("X-LLM-Gateway-Token", token)
	}
}

func llmChatRequest(input LLMChatInput) llmChatGatewayRequest {
	return llmChatGatewayRequest{
		Task:         input.Task,
		Model:        input.Model,
		SystemPrompt: input.SystemPrompt,
		Prompt:       input.Prompt,
		Messages:     llmMessages(input.Messages),
		Temperature:  input.Temperature,
		MaxTokens:    input.MaxTokens,
		RequiresJSON: true,
	}
}

func llmMessages(messages []LLMMessage) []llmGatewayMessage {
	out := make([]llmGatewayMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, llmGatewayMessage{Role: msg.Role, Content: msg.Content})
	}
	return out
}

func llmChatOutput(resp llmChatResponse) LLMChatOutput {
	return LLMChatOutput{
		Model:        resp.Model,
		Provider:     resp.Provider,
		Content:      resp.Content,
		FinishReason: resp.FinishReason,
		LatencyMs:    resp.LatencyMs,
		Usage:        llmUsage(resp.Usage),
		CostUSD:      resp.CostUSD,
		TraceID:      resp.TraceID,
		FallbackFrom: resp.FallbackFrom,
	}
}

func llmEvaluationResult(resp llmEvaluationResultDTO) LLMEvaluationResult {
	return LLMEvaluationResult{
		Model:        resp.Model,
		Provider:     resp.Provider,
		Content:      resp.Content,
		FinishReason: resp.FinishReason,
		LatencyMs:    resp.LatencyMs,
		Usage:        llmUsage(resp.Usage),
		CostUSD:      resp.CostUSD,
		Error:        resp.Error,
	}
}

func llmUsage(usage *llmUsageDTO) LLMUsage {
	if usage == nil {
		return LLMUsage{}
	}
	return LLMUsage{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
	}
}

type llmGatewayMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type llmChatGatewayRequest struct {
	Task         string              `json:"task,omitempty"`
	Model        string              `json:"model,omitempty"`
	SystemPrompt string              `json:"system_prompt,omitempty"`
	Prompt       string              `json:"prompt,omitempty"`
	Messages     []llmGatewayMessage `json:"messages,omitempty"`
	Temperature  float32             `json:"temperature,omitempty"`
	MaxTokens    int32               `json:"max_tokens,omitempty"`
	RequiresJSON bool                `json:"requires_json,omitempty"`
}

type llmEvaluateRequest struct {
	Task         string              `json:"task,omitempty"`
	Models       []string            `json:"models,omitempty"`
	SystemPrompt string              `json:"system_prompt,omitempty"`
	Prompt       string              `json:"prompt,omitempty"`
	Messages     []llmGatewayMessage `json:"messages,omitempty"`
	Temperature  float32             `json:"temperature,omitempty"`
	MaxTokens    int32               `json:"max_tokens,omitempty"`
}

type llmUsageDTO struct {
	PromptTokens     int32 `json:"prompt_tokens,omitempty"`
	CompletionTokens int32 `json:"completion_tokens,omitempty"`
	TotalTokens      int32 `json:"total_tokens,omitempty"`
}

type llmChatResponse struct {
	Model        string       `json:"model"`
	Provider     string       `json:"provider"`
	Content      string       `json:"content"`
	FinishReason string       `json:"finish_reason,omitempty"`
	LatencyMs    int64        `json:"latency_ms"`
	Usage        *llmUsageDTO `json:"usage,omitempty"`
	CostUSD      float64      `json:"cost_usd,omitempty"`
	TraceID      string       `json:"trace_id,omitempty"`
	FallbackFrom string       `json:"fallback_from,omitempty"`
}

type llmEvaluationResultDTO struct {
	Model        string       `json:"model"`
	Provider     string       `json:"provider"`
	Content      string       `json:"content,omitempty"`
	FinishReason string       `json:"finish_reason,omitempty"`
	LatencyMs    int64        `json:"latency_ms"`
	Usage        *llmUsageDTO `json:"usage,omitempty"`
	CostUSD      float64      `json:"cost_usd,omitempty"`
	Error        string       `json:"error,omitempty"`
}

type llmEvaluateResponse struct {
	Results []llmEvaluationResultDTO `json:"results"`
}

type llmModelsResponse struct {
	Models []struct {
		Name       string `json:"name"`
		Provider   string `json:"provider"`
		BaseURL    string `json:"base_url"`
		Model      string `json:"model"`
		Configured bool   `json:"configured"`
	} `json:"models"`
}

type llmStreamChunk struct {
	Content      string `json:"content,omitempty"`
	Model        string `json:"model,omitempty"`
	Provider     string `json:"provider,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
	Done         bool   `json:"done"`
	TraceID      string `json:"trace_id,omitempty"`
	Error        string `json:"error,omitempty"`
}
