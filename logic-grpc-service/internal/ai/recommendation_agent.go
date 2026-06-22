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

type recommendationParsePlan struct {
	VectorQuery      string                         `json:"vector_query"`
	MustKeywords     []string                       `json:"must_keywords"`
	ShouldKeywords   []string                       `json:"should_keywords"`
	FilterConditions recommendationFilterConditions `json:"filter_conditions"`
}

type recommendationFilterConditions struct {
	MinExperienceMonths int    `json:"min_experience_months"`
	EducationDegree     string `json:"education_degree"`
}

type recommendationRerankItem struct {
	ChunkID uint64  `json:"chunk_id"`
	Score   float32 `json:"score"`
}

type recommendationRerankResponse struct {
	Items []recommendationRerankItem `json:"items"`
}

func (c *Client) parseRecommendationPlan(ctx context.Context, input ResumeRecommendationInput, jobTitle string, progress RecommendationProgressFunc) recommendationParsePlan {
	fallback := fallbackRecommendationParsePlan(input)
	if strings.TrimSpace(c.cfg.APIKey) == "" {
		return fallback
	}
	if progress != nil {
		if err := progress("jd_parse_start", "正在由大模型结构化提炼 JD 检索计划"); err != nil {
			return fallback
		}
	}
	cm, ctx, cancel, err := c.newRecommendationChatModel(ctx)
	if err != nil {
		return fallback
	}
	defer cancel()
	msg, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage(recommendationParseInstruction()),
		schema.UserMessage(recommendationParsePrompt(input, jobTitle)),
	})
	if err != nil || msg == nil {
		return fallback
	}
	plan, err := parseRecommendationPlanResponse(msg.Content)
	if err != nil || strings.TrimSpace(plan.VectorQuery) == "" {
		return fallback
	}
	if progress != nil {
		_ = progress("jd_parse_done", "已生成结构化 RAG 检索计划")
	}
	return plan
}

func (c *Client) rerankRecommendationEvidence(ctx context.Context, input ResumeRecommendationInput, jobTitle string, plan recommendationParsePlan, items []ResumeSemanticSearchItem, limit int, progress RecommendationProgressFunc) []ResumeSemanticSearchItem {
	limit = normalizeRecommendationRerankLimit(limit)
	fallback := topRecommendationEvidence(items, limit)
	if len(fallback) == 0 || strings.TrimSpace(c.cfg.APIKey) == "" {
		return fallback
	}
	if progress != nil {
		if err := progress("rerank_start", "正在由大模型 rerank 召回证据"); err != nil {
			return fallback
		}
	}
	cm, ctx, cancel, err := c.newRecommendationChatModel(ctx)
	if err != nil {
		return fallback
	}
	defer cancel()
	msg, err := cm.Generate(ctx, []*schema.Message{
		schema.SystemMessage(recommendationRerankInstruction()),
		schema.UserMessage(recommendationRerankPrompt(input, jobTitle, plan, items, recommendationRecallTopK, limit)),
	})
	if err != nil || msg == nil {
		return fallback
	}
	reranked, err := parseRecommendationRerankResponse(msg.Content)
	if err != nil || len(reranked.Items) == 0 {
		return fallback
	}
	out := applyRecommendationEvidenceRerank(items, reranked, limit)
	if len(out) == 0 {
		return fallback
	}
	if progress != nil {
		_ = progress("rerank_done", fmt.Sprintf("已 rerank 出 top%d 条候选证据", len(out)))
	}
	return out
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

	if progress != nil {
		if err := progress("model_call_start", "推荐判断模型开始调用"); err != nil {
			return fallbackRecommendationCandidates(candidates), recommendationAgentStatusRAGOnly, fmt.Sprintf("推荐判断模型开始前状态校验失败：%v", err)
		}
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

func (c *Client) newRecommendationChatModel(ctx context.Context) (*openai.ChatModel, context.Context, context.CancelFunc, error) {
	timeout := time.Duration(c.cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	cm, err := openai.NewChatModel(childCtx, &openai.ChatModelConfig{
		APIKey:  c.cfg.APIKey,
		Model:   c.cfg.Model,
		BaseURL: c.cfg.BaseURL,
		Timeout: timeout,
	})
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return cm, childCtx, cancel, nil
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

func recommendationParseInstruction() string {
	return strings.Join([]string{
		"你是招聘 RAG 系统的 JD 结构化解析器，需要把岗位 JD 提炼成检索计划 JSON。",
		"必须综合 job_description 和 hr_requirements；hr_requirements 是 HR 本次推荐的额外偏好、限制或加分项。",
		"vector_query 用自然语言描述候选人应具备的核心经历场景，不要堆砌关键词。",
		"must_keywords 放硬技能核心词；should_keywords 放加分技能、框架、项目或工具词。",
		"filter_conditions 只放可硬过滤的基础条件；无法明确判断时用 0 或空字符串。",
		"只输出 JSON，不要输出 Markdown，不要添加解释性文字。",
		"JSON schema: {\"vector_query\":\"负责高并发后端服务开发，处理过分布式一致性或海量数据处理场景，熟悉微服务架构设计。\",\"must_keywords\":[\"Go\",\"Golang\",\"Gin\",\"GORM\"],\"should_keywords\":[\"Raft\",\"MapReduce\",\"Docker\",\"Vue3\"],\"filter_conditions\":{\"min_experience_months\":3,\"education_degree\":\"bachelor\"}}",
		"education_degree 只能是 associate、bachelor、master、doctor 或空字符串。",
	}, "\n")
}

func recommendationParsePrompt(input ResumeRecommendationInput, jobTitle string) string {
	payload := struct {
		JobTitle       string   `json:"job_title,omitempty"`
		JobDescription string   `json:"job_description"`
		HRRequirements []string `json:"hr_requirements,omitempty"`
	}{
		JobTitle:       jobTitle,
		JobDescription: truncateRunes(input.JobDescription, 1800),
		HRRequirements: normalizeRecommendationQueries(input.Queries),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return "请根据以下岗位信息生成结构化 RAG 检索计划 JSON：\n" + string(data)
}

func recommendationRerankInstruction() string {
	return strings.Join([]string{
		"你是招聘 RAG 系统的证据 reranker，只能根据 JD 和候选人经历证据判断相关性。",
		"目标是从召回证据中选出最能支持候选人匹配岗位的 top20 证据。",
		"不要编造证据，不要输出未出现在输入中的 chunk_id。",
		"只输出 JSON，不要输出 Markdown，不要添加解释性文字。",
		"JSON schema: {\"items\":[{\"chunk_id\":123,\"score\":0.95}]}",
		"score 必须在 0 到 1 之间，items 最多 20 条，按相关性从高到低排序。",
	}, "\n")
}

func recommendationRerankPrompt(input ResumeRecommendationInput, jobTitle string, plan recommendationParsePlan, items []ResumeSemanticSearchItem, recallLimit, rerankLimit int) string {
	type rerankEvidence struct {
		ChunkID       uint64  `json:"chunk_id"`
		CandidateID   uint64  `json:"candidate_id"`
		CandidateName string  `json:"candidate_name,omitempty"`
		Section       string  `json:"section,omitempty"`
		RAGScore      float32 `json:"rag_score"`
		SemanticScore float32 `json:"semantic_score"`
		KeywordScore  float32 `json:"keyword_score"`
		Content       string  `json:"content"`
	}
	payload := struct {
		JobTitle       string           `json:"job_title,omitempty"`
		JobDescription string           `json:"job_description"`
		VectorQuery    string           `json:"vector_query"`
		MustKeywords   []string         `json:"must_keywords,omitempty"`
		ShouldKeywords []string         `json:"should_keywords,omitempty"`
		Limit          int              `json:"limit"`
		Evidence       []rerankEvidence `json:"evidence"`
	}{
		JobTitle:       jobTitle,
		JobDescription: truncateRunes(input.JobDescription, 1200),
		VectorQuery:    truncateRunes(plan.VectorQuery, 500),
		MustKeywords:   plan.MustKeywords,
		ShouldKeywords: plan.ShouldKeywords,
		Limit:          normalizeRecommendationRerankLimit(rerankLimit),
		Evidence:       make([]rerankEvidence, 0, minInt(len(items), recallLimit)),
	}
	items = topRecommendationEvidence(items, recallLimit)
	for _, item := range items {
		section := strings.TrimSpace(item.SectionTitle)
		if section == "" {
			section = item.SectionType
		}
		payload.Evidence = append(payload.Evidence, rerankEvidence{
			ChunkID:       item.ChunkID,
			CandidateID:   item.CandidateID,
			CandidateName: item.CandidateName,
			Section:       section,
			RAGScore:      item.Score,
			SemanticScore: item.SemanticScore,
			KeywordScore:  item.KeywordScore,
			Content:       truncateRunes(item.Content, 420),
		})
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return "请对以下召回证据进行 rerank，并返回 top evidence JSON：\n" + string(data)
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
	content = cleanRecommendationJSONContent(content)
	var out recommendationAgentResponse
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return recommendationAgentResponse{}, err
	}
	if len(out.Candidates) == 0 {
		return recommendationAgentResponse{}, fmt.Errorf("empty candidates")
	}
	return out, nil
}

func parseRecommendationPlanResponse(content string) (recommendationParsePlan, error) {
	content = cleanRecommendationJSONContent(content)
	var out recommendationParsePlan
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return recommendationParsePlan{}, err
	}
	out.VectorQuery = truncateRunes(normalizeResumeText(out.VectorQuery), 300)
	out.MustKeywords = normalizeRecommendationQueries(out.MustKeywords)
	out.ShouldKeywords = normalizeRecommendationQueries(out.ShouldKeywords)
	out.FilterConditions.EducationDegree = normalizeEducationDegree(out.FilterConditions.EducationDegree)
	if out.FilterConditions.MinExperienceMonths < 0 {
		out.FilterConditions.MinExperienceMonths = 0
	}
	if out.VectorQuery == "" {
		return recommendationParsePlan{}, fmt.Errorf("empty vector_query")
	}
	return out, nil
}

func parseRecommendationRerankResponse(content string) (recommendationRerankResponse, error) {
	content = cleanRecommendationJSONContent(content)
	var out recommendationRerankResponse
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return recommendationRerankResponse{}, err
	}
	if len(out.Items) == 0 {
		return recommendationRerankResponse{}, fmt.Errorf("empty rerank items")
	}
	return out, nil
}

func cleanRecommendationJSONContent(content string) string {
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
	return content
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

func topRecommendationEvidence(items []ResumeSemanticSearchItem, limit int) []ResumeSemanticSearchItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]ResumeSemanticSearchItem, len(items))
	copy(out, items)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func applyRecommendationEvidenceRerank(items []ResumeSemanticSearchItem, reranked recommendationRerankResponse, limit int) []ResumeSemanticSearchItem {
	byChunkID := make(map[uint64]ResumeSemanticSearchItem, len(items))
	for _, item := range items {
		if item.ChunkID == 0 {
			continue
		}
		byChunkID[item.ChunkID] = item
	}
	out := make([]ResumeSemanticSearchItem, 0, minInt(limit, len(reranked.Items)))
	seen := map[uint64]struct{}{}
	for _, ranked := range reranked.Items {
		if _, ok := seen[ranked.ChunkID]; ok {
			continue
		}
		item, ok := byChunkID[ranked.ChunkID]
		if !ok {
			continue
		}
		item.Score = clampFloat32(ranked.Score, 0, 1)
		out = append(out, item)
		seen[ranked.ChunkID] = struct{}{}
		if len(out) >= limit {
			break
		}
	}
	return out
}

func normalizeRecommendationRerankLimit(limit int) int {
	if limit <= 0 || limit > recommendationRerankTopK {
		return recommendationRerankTopK
	}
	return limit
}

func fallbackRecommendationParsePlan(input ResumeRecommendationInput) recommendationParsePlan {
	queries := recommendationQueries(input)
	vectorQuery := strings.Join(queries, "\n")
	keywords := normalizeRecommendationQueries(input.Queries)
	if len(keywords) == 0 {
		keywords = extractFallbackKeywords(vectorQuery)
	}
	return recommendationParsePlan{
		VectorQuery:    truncateRunes(normalizeResumeText(vectorQuery), 300),
		MustKeywords:   keywords,
		ShouldKeywords: nil,
	}
}

func extractFallbackKeywords(text string) []string {
	text = normalizeResumeText(text)
	keywords := make([]string, 0, 8)
	for _, keyword := range resumeKeywordVocabulary {
		if strings.Contains(strings.ToLower(text), strings.ToLower(keyword)) {
			keywords = append(keywords, keyword)
			if len(keywords) >= 8 {
				break
			}
		}
	}
	return normalizeRecommendationQueries(keywords)
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
