package gateway

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"recruitment/llm-gateway-service/internal/config"
)

type Runtime struct {
	cfg      config.Config
	models   map[string]config.ModelConfig
	order    []string
	limiter  *RateLimiter
	breaker  *CircuitBreaker
	stats    *Stats
	provider *OpenAICompatibleProvider
}

func NewRuntime(cfg config.Config) *Runtime {
	models := make(map[string]config.ModelConfig, len(cfg.Models))
	order := make([]string, 0, len(cfg.Models))
	for _, model := range cfg.Models {
		models[strings.ToLower(model.Name)] = model
		if model.Model != "" {
			models[strings.ToLower(model.Model)] = model
		}
		order = append(order, model.Name)
	}
	return &Runtime{
		cfg:      cfg,
		models:   models,
		order:    order,
		limiter:  NewRateLimiter(),
		breaker:  NewCircuitBreaker(3, 45*time.Second),
		stats:    NewStats(),
		provider: NewOpenAICompatibleProvider(cfg.DefaultTimeout),
	}
}

func (r *Runtime) Models() []ModelInfo {
	out := make([]ModelInfo, 0, len(r.cfg.Models))
	for _, model := range r.cfg.Models {
		out = append(out, ModelInfo{
			Name:                 model.Name,
			Provider:             model.Provider,
			BaseURL:              model.BaseURL,
			Model:                model.Model,
			Configured:           modelConfigured(model),
			ContextWindow:        model.ContextWindow,
			MaxOutputTokens:      model.MaxOutputTokens,
			RPM:                  model.RPM,
			TPM:                  model.TPM,
			SupportsStreaming:    model.SupportsStreaming,
			SupportsJSONResponse: model.SupportsJSONResponse,
			Weight:               model.Weight,
			InputPricePer1KUSD:   model.InputPricePer1KUSD,
			OutputPricePer1KUSD:  model.OutputPricePer1KUSD,
		})
	}
	return out
}

func (r *Runtime) Stats() StatsResponse {
	return r.stats.Snapshot()
}

func (r *Runtime) ResolveCandidates(req ChatRequest, stream bool) ([]config.ModelConfig, error) {
	if strings.TrimSpace(req.Model) != "" {
		model, err := r.model(req.Model)
		if err != nil {
			return nil, err
		}
		return []config.ModelConfig{model}, nil
	}
	route := r.cfg.TaskRoutes[strings.TrimSpace(req.Task)]
	names := make([]string, 0, 1+len(route.Fallbacks)+len(r.order))
	if route.Primary != "" {
		names = append(names, route.Primary)
	}
	names = append(names, route.Fallbacks...)
	if len(names) == 0 {
		names = append(names, r.order...)
	}
	seen := map[string]struct{}{}
	out := make([]config.ModelConfig, 0, len(names))
	for _, name := range names {
		model, err := r.model(name)
		if err != nil {
			continue
		}
		key := strings.ToLower(model.Name)
		if _, ok := seen[key]; ok {
			continue
		}
		if stream && !model.SupportsStreaming {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no available model candidates for task %q", req.Task)
	}
	return r.sortCandidates(out, req), nil
}

func (r *Runtime) sortCandidates(models []config.ModelConfig, req ChatRequest) []config.ModelConfig {
	estimate := estimateTokens(req)
	sort.SliceStable(models, func(i, j int) bool {
		left := r.modelPenalty(models[i], estimate)
		right := r.modelPenalty(models[j], estimate)
		if left == right {
			return models[i].Weight > models[j].Weight
		}
		return left < right
	})
	return models
}

func (r *Runtime) modelPenalty(model config.ModelConfig, estimate int) float64 {
	penalty := 0.0
	if r.breaker.Open(model.Name) {
		penalty += 100000
	}
	if !modelConfigured(model) {
		penalty += 100000
	}
	if model.ContextWindow > 0 && estimate > model.ContextWindow {
		penalty += 50000
	}
	if r.limiter.NearLimit(model.Name) {
		penalty += 1000
	}
	weight := math.Max(float64(model.Weight), 1)
	return penalty + (1 / weight)
}

func (r *Runtime) model(name string) (config.ModelConfig, error) {
	model, ok := r.models[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return config.ModelConfig{}, fmt.Errorf("model %q not found", name)
	}
	return model, nil
}

func (r *Runtime) route(task string) config.TaskRoute {
	return r.cfg.TaskRoutes[strings.TrimSpace(task)]
}

func modelConfigured(model config.ModelConfig) bool {
	return strings.TrimSpace(model.APIKey) != "" && strings.TrimSpace(model.BaseURL) != "" && strings.TrimSpace(model.Model) != ""
}

func estimateTokens(req ChatRequest) int {
	chars := len(req.SystemPrompt) + len(req.Prompt)
	for _, msg := range req.Messages {
		chars += len(msg.Content)
	}
	estimate := chars / 4
	if estimate <= 0 {
		estimate = 1
	}
	if req.MaxTokens > 0 {
		estimate += int(req.MaxTokens)
	}
	return estimate
}

type RateLimiter struct {
	mu      sync.Mutex
	windows map[string]*limitWindow
}

type limitWindow struct {
	start  time.Time
	rpm    int
	tokens int
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{windows: map[string]*limitWindow{}}
}

func (l *RateLimiter) Allow(model config.ModelConfig, tokens int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	window := l.window(model.Name)
	if model.RPM > 0 && window.rpm+1 > model.RPM {
		return false
	}
	if model.TPM > 0 && window.tokens+tokens > model.TPM {
		return false
	}
	window.rpm++
	window.tokens += tokens
	return true
}

func (l *RateLimiter) NearLimit(modelName string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	window := l.window(modelName)
	return window.rpm > 0 && window.tokens > 0
}

func (l *RateLimiter) window(modelName string) *limitWindow {
	now := time.Now()
	key := strings.ToLower(modelName)
	window := l.windows[key]
	if window == nil || now.Sub(window.start) >= time.Minute {
		window = &limitWindow{start: now}
		l.windows[key] = window
	}
	return window
}

type CircuitBreaker struct {
	mu         sync.Mutex
	threshold  int
	openFor    time.Duration
	failures   map[string]int
	openedTill map[string]time.Time
}

func NewCircuitBreaker(threshold int, openFor time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold:  threshold,
		openFor:    openFor,
		failures:   map[string]int{},
		openedTill: map[string]time.Time{},
	}
}

func (b *CircuitBreaker) Open(modelName string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	until := b.openedTill[strings.ToLower(modelName)]
	return !until.IsZero() && time.Now().Before(until)
}

func (b *CircuitBreaker) Success(modelName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := strings.ToLower(modelName)
	delete(b.failures, key)
	delete(b.openedTill, key)
}

func (b *CircuitBreaker) Failure(modelName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := strings.ToLower(modelName)
	b.failures[key]++
	if b.failures[key] >= b.threshold {
		b.openedTill[key] = time.Now().Add(b.openFor)
	}
}

type Stats struct {
	mu        sync.Mutex
	requests  int64
	failures  int64
	fallbacks int64
	byModel   map[string]*modelStats
}

type modelStats struct {
	requests         int64
	failures         int64
	promptTokens     int64
	completionTokens int64
	costUSD          float64
}

func NewStats() *Stats {
	return &Stats{byModel: map[string]*modelStats{}}
}

func (s *Stats) RecordSuccess(model config.ModelConfig, usage *Usage, cost float64, fallback bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	if fallback {
		s.fallbacks++
	}
	item := s.item(model.Name)
	item.requests++
	if usage != nil {
		item.promptTokens += int64(usage.PromptTokens)
		item.completionTokens += int64(usage.CompletionTokens)
	}
	item.costUSD += cost
}

func (s *Stats) RecordFailure(model config.ModelConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	s.failures++
	item := s.item(model.Name)
	item.requests++
	item.failures++
}

func (s *Stats) Snapshot() StatsResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	resp := StatsResponse{Requests: s.requests, Failures: s.failures, Fallbacks: s.fallbacks, ByModel: map[string]ModelStatsDTO{}}
	for name, item := range s.byModel {
		resp.PromptTokens += item.promptTokens
		resp.CompletionTokens += item.completionTokens
		resp.TotalCostUSD += item.costUSD
		resp.ByModel[name] = ModelStatsDTO{
			Requests:         item.requests,
			Failures:         item.failures,
			PromptTokens:     item.promptTokens,
			CompletionTokens: item.completionTokens,
			TotalCostUSD:     item.costUSD,
		}
	}
	return resp
}

func (s *Stats) item(modelName string) *modelStats {
	item := s.byModel[modelName]
	if item == nil {
		item = &modelStats{}
		s.byModel[modelName] = item
	}
	return item
}

func estimateCost(model config.ModelConfig, usage *Usage) float64 {
	if usage == nil {
		return 0
	}
	return float64(usage.PromptTokens)/1000*model.InputPricePer1KUSD +
		float64(usage.CompletionTokens)/1000*model.OutputPricePer1KUSD
}
