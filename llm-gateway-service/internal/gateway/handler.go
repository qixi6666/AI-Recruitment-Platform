package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"recruitment/llm-gateway-service/internal/config"
)

type Handler struct {
	rt       *Runtime
	apiToken string
}

func NewHandler(rt *Runtime, apiToken string) *Handler {
	return &Handler{rt: rt, apiToken: apiToken}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	api := r.Group("/api/v1")
	api.Use(h.auth())
	api.GET("/models", h.ListModels)
	api.GET("/stats", h.Stats)
	api.POST("/chat", h.Chat)
	api.POST("/evaluate", h.Evaluate)
	api.POST("/stream", h.Stream)
}

func (h *Handler) ListModels(c *gin.Context) {
	c.JSON(http.StatusOK, ModelsResponse{Models: h.rt.Models()})
}

func (h *Handler) Stats(c *gin.Context) {
	c.JSON(http.StatusOK, h.rt.Stats())
}

func (h *Handler) Chat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.TraceID = traceID(c, req.TraceID)
	resp, err := h.executeChat(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "trace_id": req.TraceID})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) Evaluate(c *gin.Context) {
	var req EvaluateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	trace := traceID(c, req.TraceID)
	results := make([]*EvaluationResult, len(req.Models))
	if len(req.Models) == 0 {
		candidates, err := h.rt.ResolveCandidates(ChatRequest{Task: req.Task, MaxTokens: req.MaxTokens, Messages: req.Messages, Prompt: req.Prompt, SystemPrompt: req.SystemPrompt}, false)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "trace_id": trace})
			return
		}
		req.Models = make([]string, 0, len(candidates))
		for _, model := range candidates {
			req.Models = append(req.Models, model.Name)
		}
		results = make([]*EvaluationResult, len(req.Models))
	}
	var wg sync.WaitGroup
	for i, modelName := range req.Models {
		i, modelName := i, modelName
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := h.executeChat(c.Request.Context(), ChatRequest{
				Task:         req.Task,
				Model:        modelName,
				SystemPrompt: req.SystemPrompt,
				Prompt:       req.Prompt,
				Messages:     req.Messages,
				Temperature:  req.Temperature,
				MaxTokens:    req.MaxTokens,
				UserID:       req.UserID,
				TenantID:     req.TenantID,
				TraceID:      trace,
				RequiresJSON: req.RequiresJSON,
			})
			result := &EvaluationResult{Model: modelName}
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Model = resp.Model
				result.Provider = resp.Provider
				result.Content = resp.Content
				result.FinishReason = resp.FinishReason
				result.LatencyMs = resp.LatencyMs
				result.Usage = resp.Usage
				result.CostUSD = resp.CostUSD
			}
			results[i] = result
		}()
	}
	wg.Wait()
	c.JSON(http.StatusOK, EvaluateResponse{TraceID: trace, Results: results})
}

func (h *Handler) Stream(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.TraceID = traceID(c, req.TraceID)
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}
	if err := h.executeStream(c, req, flusher); err != nil {
		writeSSE(c.Writer, "error", StreamChunk{Error: err.Error(), TraceID: req.TraceID, Done: true})
		flusher.Flush()
		return
	}
}

func (h *Handler) executeChat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	candidates, err := h.rt.ResolveCandidates(req, false)
	if err != nil {
		return ChatResponse{}, err
	}
	estimate := estimateTokens(req)
	var lastErr error
	for i, model := range candidates {
		if i > 0 && !h.rt.route(req.Task).AllowFallback {
			break
		}
		if h.rt.breaker.Open(model.Name) {
			continue
		}
		if !modelConfigured(model) {
			lastErr = fmt.Errorf("model %s is not fully configured", model.Name)
			continue
		}
		if !h.rt.limiter.Allow(model, estimate) {
			lastErr = fmt.Errorf("model %s exceeded rpm/tpm limit", model.Name)
			continue
		}
		resp, err := h.rt.provider.Chat(ctx, model, req)
		if err != nil {
			h.rt.breaker.Failure(model.Name)
			h.rt.stats.RecordFailure(model)
			lastErr = convertModelError(model, err)
			continue
		}
		h.rt.breaker.Success(model.Name)
		resp.CostUSD = estimateCost(model, resp.Usage)
		if i > 0 {
			resp.FallbackFrom = candidates[0].Name
		}
		h.rt.stats.RecordSuccess(model, resp.Usage, resp.CostUSD, i > 0)
		return resp, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no available model")
	}
	return ChatResponse{}, lastErr
}

func (h *Handler) executeStream(c *gin.Context, req ChatRequest, flusher http.Flusher) error {
	candidates, err := h.rt.ResolveCandidates(req, true)
	if err != nil {
		return err
	}
	var lastErr error
	estimate := estimateTokens(req)
	for i, model := range candidates {
		if i > 0 && !h.rt.route(req.Task).AllowFallback {
			break
		}
		if h.rt.breaker.Open(model.Name) {
			continue
		}
		if !modelConfigured(model) {
			lastErr = fmt.Errorf("model %s is not fully configured", model.Name)
			continue
		}
		if !h.rt.limiter.Allow(model, estimate) {
			lastErr = fmt.Errorf("model %s exceeded rpm/tpm limit", model.Name)
			continue
		}
		wrote := false
		resp, err := h.rt.provider.Stream(c.Request.Context(), model, req, func(chunk StreamChunk) error {
			wrote = true
			writeSSE(c.Writer, "delta", chunk)
			flusher.Flush()
			return nil
		})
		if err != nil {
			h.rt.breaker.Failure(model.Name)
			h.rt.stats.RecordFailure(model)
			lastErr = err
			if wrote || !h.rt.route(req.Task).AllowStreamSwitch {
				return err
			}
			continue
		}
		h.rt.breaker.Success(model.Name)
		cost := estimateCost(model, resp.Usage)
		h.rt.stats.RecordSuccess(model, resp.Usage, cost, i > 0)
		writeSSE(c.Writer, "done", StreamChunk{
			Model:        model.Name,
			Provider:     model.Provider,
			FinishReason: resp.FinishReason,
			Done:         true,
			TraceID:      req.TraceID,
		})
		flusher.Flush()
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no available stream model")
	}
	return lastErr
}

func (h *Handler) auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.TrimSpace(h.apiToken) == "" {
			c.Next()
			return
		}
		token := c.GetHeader("X-LLM-Gateway-Token")
		if token == "" {
			token = strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		}
		if token != h.apiToken {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid llm gateway token"})
			return
		}
		c.Next()
	}
}

func traceID(c *gin.Context, explicit string) string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit
	}
	if value := strings.TrimSpace(c.GetHeader("X-Trace-ID")); value != "" {
		return value
	}
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

func writeSSE(w http.ResponseWriter, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(`{"error":"marshal sse payload failed"}`)
	}
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func convertModelError(model config.ModelConfig, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s/%s: %w", model.Provider, model.Name, err)
}
