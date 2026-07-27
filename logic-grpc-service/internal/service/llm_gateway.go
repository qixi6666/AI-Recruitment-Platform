package service

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"recruitment/logic-grpc-service/internal/ai"
	"recruitment/logic-grpc-service/internal/domain"
	"recruitment/shared/rpc"
)

func (s *Server) ListModels(ctx context.Context, req *rpc.ListLLMModelsRequest) (*rpc.ListLLMModelsResponse, error) {
	if err := requireRole(req.Actor, domain.RoleHR); err != nil {
		return nil, err
	}
	models := s.ai.ListLLMModels()
	resp := &rpc.ListLLMModelsResponse{Models: make([]*rpc.LLMModelDTO, 0, len(models))}
	for _, model := range models {
		resp.Models = append(resp.Models, llmModelDTO(model))
	}
	return resp, nil
}

func (s *Server) Chat(ctx context.Context, req *rpc.LLMChatRequest) (*rpc.LLMChatResponse, error) {
	if err := requireRole(req.Actor, domain.RoleHR); err != nil {
		return nil, err
	}
	output, err := s.ai.ChatLLM(ctx, llmChatInput(req.Model, req.SystemPrompt, req.Prompt, req.Messages, req.Temperature, req.MaxTokens))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return llmChatResponse(output), nil
}

func (s *Server) Evaluate(ctx context.Context, req *rpc.LLMEvaluateRequest) (*rpc.LLMEvaluateResponse, error) {
	if err := requireRole(req.Actor, domain.RoleHR); err != nil {
		return nil, err
	}
	results, err := s.ai.EvaluateLLM(ctx, llmChatInput("", req.SystemPrompt, req.Prompt, req.Messages, req.Temperature, req.MaxTokens), req.Models)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	resp := &rpc.LLMEvaluateResponse{Results: make([]*rpc.LLMEvaluationResultDTO, 0, len(results))}
	for _, result := range results {
		resp.Results = append(resp.Results, llmEvaluationResultDTO(result))
	}
	return resp, nil
}

func llmChatInput(model, systemPrompt, prompt string, messages []*rpc.LLMMessageDTO, temperature float32, maxTokens int32) ai.LLMChatInput {
	input := ai.LLMChatInput{
		Model:        model,
		SystemPrompt: systemPrompt,
		Prompt:       prompt,
		Temperature:  temperature,
		MaxTokens:    maxTokens,
		Messages:     make([]ai.LLMMessage, 0, len(messages)),
	}
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		input.Messages = append(input.Messages, ai.LLMMessage{Role: msg.Role, Content: msg.Content})
	}
	return input
}

func llmModelDTO(model ai.LLMModel) *rpc.LLMModelDTO {
	return &rpc.LLMModelDTO{
		Name:       model.Name,
		Provider:   model.Provider,
		BaseURL:    model.BaseURL,
		Model:      model.Model,
		Configured: model.Configured,
	}
}

func llmChatResponse(output ai.LLMChatOutput) *rpc.LLMChatResponse {
	return &rpc.LLMChatResponse{
		Model:        output.Model,
		Provider:     output.Provider,
		Content:      output.Content,
		FinishReason: output.FinishReason,
		LatencyMs:    output.LatencyMs,
		Usage:        llmUsageDTO(output.Usage),
	}
}

func llmEvaluationResultDTO(result ai.LLMEvaluationResult) *rpc.LLMEvaluationResultDTO {
	return &rpc.LLMEvaluationResultDTO{
		Model:        result.Model,
		Provider:     result.Provider,
		Content:      result.Content,
		FinishReason: result.FinishReason,
		LatencyMs:    result.LatencyMs,
		Usage:        llmUsageDTO(result.Usage),
		Error:        result.Error,
	}
}

func llmUsageDTO(usage ai.LLMUsage) *rpc.LLMUsageDTO {
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 {
		return nil
	}
	return &rpc.LLMUsageDTO{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
	}
}
