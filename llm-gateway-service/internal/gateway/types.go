package gateway

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Task         string    `json:"task,omitempty"`
	Model        string    `json:"model,omitempty"`
	SystemPrompt string    `json:"system_prompt,omitempty"`
	Prompt       string    `json:"prompt,omitempty"`
	Messages     []Message `json:"messages,omitempty"`
	Temperature  float32   `json:"temperature,omitempty"`
	MaxTokens    int32     `json:"max_tokens,omitempty"`
	UserID       string    `json:"user_id,omitempty"`
	TenantID     string    `json:"tenant_id,omitempty"`
	TraceID      string    `json:"trace_id,omitempty"`
	RequiresJSON bool      `json:"requires_json,omitempty"`
}

type EvaluateRequest struct {
	Task         string    `json:"task,omitempty"`
	Models       []string  `json:"models,omitempty"`
	SystemPrompt string    `json:"system_prompt,omitempty"`
	Prompt       string    `json:"prompt,omitempty"`
	Messages     []Message `json:"messages,omitempty"`
	Temperature  float32   `json:"temperature,omitempty"`
	MaxTokens    int32     `json:"max_tokens,omitempty"`
	UserID       string    `json:"user_id,omitempty"`
	TenantID     string    `json:"tenant_id,omitempty"`
	TraceID      string    `json:"trace_id,omitempty"`
	RequiresJSON bool      `json:"requires_json,omitempty"`
}

type Usage struct {
	PromptTokens     int32 `json:"prompt_tokens,omitempty"`
	CompletionTokens int32 `json:"completion_tokens,omitempty"`
	TotalTokens      int32 `json:"total_tokens,omitempty"`
}

type ChatResponse struct {
	Model        string  `json:"model"`
	Provider     string  `json:"provider"`
	Content      string  `json:"content"`
	FinishReason string  `json:"finish_reason,omitempty"`
	LatencyMs    int64   `json:"latency_ms"`
	Usage        *Usage  `json:"usage,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	TraceID      string  `json:"trace_id,omitempty"`
	FallbackFrom string  `json:"fallback_from,omitempty"`
}

type EvaluationResult struct {
	Model        string  `json:"model"`
	Provider     string  `json:"provider"`
	Content      string  `json:"content,omitempty"`
	FinishReason string  `json:"finish_reason,omitempty"`
	LatencyMs    int64   `json:"latency_ms"`
	Usage        *Usage  `json:"usage,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	Error        string  `json:"error,omitempty"`
}

type EvaluateResponse struct {
	TraceID string              `json:"trace_id,omitempty"`
	Results []*EvaluationResult `json:"results"`
}

type ModelInfo struct {
	Name                 string  `json:"name"`
	Provider             string  `json:"provider"`
	BaseURL              string  `json:"base_url"`
	Model                string  `json:"model"`
	Configured           bool    `json:"configured"`
	ContextWindow        int     `json:"context_window"`
	MaxOutputTokens      int     `json:"max_output_tokens"`
	RPM                  int     `json:"rpm"`
	TPM                  int     `json:"tpm"`
	SupportsStreaming    bool    `json:"supports_streaming"`
	SupportsJSONResponse bool    `json:"supports_json_response"`
	Weight               int     `json:"weight"`
	InputPricePer1KUSD   float64 `json:"input_price_per_1k_usd,omitempty"`
	OutputPricePer1KUSD  float64 `json:"output_price_per_1k_usd,omitempty"`
}

type ModelsResponse struct {
	Models []ModelInfo `json:"models"`
}

type StreamChunk struct {
	Content      string `json:"content,omitempty"`
	Model        string `json:"model,omitempty"`
	Provider     string `json:"provider,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
	Done         bool   `json:"done"`
	TraceID      string `json:"trace_id,omitempty"`
	Error        string `json:"error,omitempty"`
}

type StatsResponse struct {
	Requests         int64                    `json:"requests"`
	Failures         int64                    `json:"failures"`
	Fallbacks        int64                    `json:"fallbacks"`
	PromptTokens     int64                    `json:"prompt_tokens"`
	CompletionTokens int64                    `json:"completion_tokens"`
	TotalCostUSD     float64                  `json:"total_cost_usd"`
	ByModel          map[string]ModelStatsDTO `json:"by_model"`
}

type ModelStatsDTO struct {
	Requests         int64   `json:"requests"`
	Failures         int64   `json:"failures"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalCostUSD     float64 `json:"total_cost_usd"`
}
