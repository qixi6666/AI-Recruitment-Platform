package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
)

const (
	recommendationAgentStatusJudged  = "agent_judged"
	recommendationAgentStatusRAGOnly = "rag_only"
)

type recommendationAgentCandidate struct {
	CandidateID uint64   `json:"candidate_id"`
	Score       float32  `json:"score"`
	Reason      string   `json:"reason"`
	RiskPoints  []string `json:"risk_points"`
}

type recommendationAgentResponse struct {
	Candidates []recommendationAgentCandidate `json:"candidates"`
}

func (c *Client) judgeRecommendationCandidates(ctx context.Context, input ResumeRecommendationInput, jobTitle string, candidates []ResumeRecommendationCandidate, progress RecommendationProgressFunc) ([]ResumeRecommendationCandidate, string, string) {
	if len(candidates) == 0 {
		return candidates, recommendationAgentStatusRAGOnly, "RAG 未召回候选人"
	}
	if strings.TrimSpace(c.cfg.APIKey) == "" {
		return fallbackRecommendationCandidates(candidates), recommendationAgentStatusRAGOnly, "未配置推荐判断模型，已返回 RAG 排序结果"
	}

	timeout := time.Duration(c.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  c.cfg.APIKey,
		Model:   c.cfg.Model,
		BaseURL: c.cfg.BaseURL,
		Timeout: timeout,
	})
	if err != nil {
		return fallbackRecommendationCandidates(candidates), recommendationAgentStatusRAGOnly, fmt.Sprintf("推荐判断模型初始化失败：%v", err)
	}

	stream, err := cm.Stream(ctx, []*schema.Message{
		schema.SystemMessage(recommendationAgentInstruction()),
		schema.UserMessage(recommendationAgentPrompt(input, jobTitle, candidates)),
	})
	if err != nil {
		return fallbackRecommendationCandidates(candidates), recommendationAgentStatusRAGOnly, recommendationAgentFallbackReason("推荐判断模型调用失败", err)
	}
	defer stream.Close()

	var content strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fallbackRecommendationCandidates(candidates), recommendationAgentStatusRAGOnly, recommendationAgentFallbackReason("推荐判断模型流式调用失败", err)
		}
		if msg == nil || msg.Content == "" {
			continue
		}
		content.WriteString(msg.Content)
		if progress != nil {
			if err := progress("final_answer_streaming", msg.Content); err != nil {
				return fallbackRecommendationCandidates(candidates), recommendationAgentStatusRAGOnly, fmt.Sprintf("推荐判断模型流式输出失败：%v", err)
			}
		}
	}

	judged, err := parseRecommendationAgentResponse(content.String())
	if err != nil {
		return fallbackRecommendationCandidates(candidates), recommendationAgentStatusRAGOnly, fmt.Sprintf("推荐判断模型返回格式无效：%v", err)
	}
	return applyRecommendationAgentJudgement(candidates, judged), recommendationAgentStatusJudged, ""
}

func recommendationAgentFallbackReason(prefix string, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "推荐判断模型超时，已返回 RAG 检索候选人兜底结果"
	}
	errText := strings.ToLower(err.Error())
	if strings.Contains(errText, "429") || strings.Contains(errText, "too many requests") || strings.Contains(errText, "rate limit") {
		return "推荐判断模型触发限流，已返回 RAG 检索候选人兜底结果"
	}
	return fmt.Sprintf("%s：%v，已返回 RAG 检索候选人兜底结果", prefix, err)
}

func recommendationAgentInstruction() string {
	return strings.Join([]string{
		"你是招聘系统里的简历推荐判断 agent，只能根据输入的 JD 和候选人证据片段做判断。",
		"不要编造候选人经历，不要使用证据之外的信息。",
		"请输出 JSON，不要输出 Markdown，不要添加解释性前后缀。",
		"JSON schema: {\"candidates\":[{\"candidate_id\":123,\"score\":0.86,\"reason\":\"推荐理由\",\"risk_points\":[\"风险点\"]}]}",
		"score 必须在 0 到 1 之间；reason 用中文，简洁说明匹配点；risk_points 最多 3 条。",
	}, "\n")
}

func recommendationAgentPrompt(input ResumeRecommendationInput, jobTitle string, candidates []ResumeRecommendationCandidate) string {
	payload := struct {
		JobTitle       string                    `json:"job_title,omitempty"`
		JobDescription string                    `json:"job_description"`
		Candidates     []recommendationCandidate `json:"candidates"`
	}{
		JobTitle:       jobTitle,
		JobDescription: truncateRunes(input.JobDescription, 1200),
		Candidates:     make([]recommendationCandidate, 0, len(candidates)),
	}
	for _, candidate := range candidates {
		item := recommendationCandidate{
			CandidateID:   candidate.CandidateID,
			CandidateName: candidate.CandidateName,
			RAGScore:      candidate.Score,
			SemanticScore: candidate.SemanticScore,
			KeywordScore:  candidate.KeywordScore,
			Evidence:      make([]recommendationEvidence, 0, len(candidate.Evidence)),
		}
		for _, evidence := range candidate.Evidence {
			item.Evidence = append(item.Evidence, recommendationEvidence{
				Section: evidence.SectionTitle,
				Content: truncateRunes(evidence.Content, 500),
			})
		}
		payload.Candidates = append(payload.Candidates, item)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return "请按匹配程度重新判断候选人，并返回 JSON：\n" + string(data)
}

type recommendationCandidate struct {
	CandidateID   uint64                   `json:"candidate_id"`
	CandidateName string                   `json:"candidate_name"`
	RAGScore      float32                  `json:"rag_score"`
	SemanticScore float32                  `json:"semantic_score"`
	KeywordScore  float32                  `json:"keyword_score"`
	Evidence      []recommendationEvidence `json:"evidence"`
}

type recommendationEvidence struct {
	Section string `json:"section"`
	Content string `json:"content"`
}

func parseRecommendationAgentResponse(content string) (recommendationAgentResponse, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	if start := strings.Index(content, "{"); start > 0 {
		content = content[start:]
	}
	if end := strings.LastIndex(content, "}"); end >= 0 && end < len(content)-1 {
		content = content[:end+1]
	}
	var out recommendationAgentResponse
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return recommendationAgentResponse{}, err
	}
	if len(out.Candidates) == 0 {
		return recommendationAgentResponse{}, fmt.Errorf("empty candidates")
	}
	return out, nil
}

func applyRecommendationAgentJudgement(candidates []ResumeRecommendationCandidate, judged recommendationAgentResponse) []ResumeRecommendationCandidate {
	byID := make(map[uint64]recommendationAgentCandidate, len(judged.Candidates))
	for _, item := range judged.Candidates {
		byID[item.CandidateID] = item
	}
	out := fallbackRecommendationCandidates(candidates)
	for i := range out {
		if item, ok := byID[out[i].CandidateID]; ok {
			out[i].Score = clampFloat32(item.Score, 0, 1)
			if strings.TrimSpace(item.Reason) != "" {
				out[i].Reason = truncateRunes(item.Reason, 260)
			}
			out[i].RiskPoints = cleanRiskPoints(item.RiskPoints)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	return out
}

func fallbackRecommendationCandidates(candidates []ResumeRecommendationCandidate) []ResumeRecommendationCandidate {
	out := make([]ResumeRecommendationCandidate, len(candidates))
	copy(out, candidates)
	for i := range out {
		if strings.TrimSpace(out[i].Reason) == "" {
			out[i].Reason = fallbackRecommendationReason(out[i])
		}
		if len(out[i].RiskPoints) == 0 {
			out[i].RiskPoints = fallbackRiskPoints(out[i])
		}
	}
	return out
}

func fallbackRecommendationReason(candidate ResumeRecommendationCandidate) string {
	if len(candidate.Evidence) == 0 {
		return "RAG 召回该候选人，但当前缺少可展示的经历证据。"
	}
	top := candidate.Evidence[0]
	section := strings.TrimSpace(top.SectionTitle)
	if section == "" {
		section = strings.TrimSpace(top.SectionType)
	}
	content := truncateRunes(top.Content, 120)
	if section == "" {
		return "RAG 召回的经历证据与岗位要求存在语义或关键词匹配：" + content
	}
	return fmt.Sprintf("RAG 召回的「%s」证据与岗位要求匹配：%s", section, content)
}

func fallbackRiskPoints(candidate ResumeRecommendationCandidate) []string {
	if len(candidate.Evidence) < 2 {
		return []string{"当前可用证据片段较少，建议结合完整简历和面试继续确认。"}
	}
	return []string{"推荐基于候选人自填经历证据，仍需结合完整简历和面试确认真实性与深度。"}
}

func cleanRiskPoints(items []string) []string {
	out := make([]string, 0, 3)
	for _, item := range items {
		item = truncateRunes(strings.TrimSpace(item), 160)
		if item == "" {
			continue
		}
		out = append(out, item)
		if len(out) >= 3 {
			break
		}
	}
	return out
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
