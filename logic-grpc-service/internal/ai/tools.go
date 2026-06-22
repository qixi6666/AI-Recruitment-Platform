package ai

import (
	"context"
	"fmt"
	"strings"

	"recruitment/logic-grpc-service/internal/domain"
)

type ResumeSemanticSearchInput struct {
	Query       string `json:"query" jsonschema:"description=用于语义检索候选人填写的项目/工作经历，例如 高并发订单系统经验、支付系统、gRPC 微服务治理"`
	JobID       uint64 `json:"job_id" jsonschema:"description=可选，限定只检索某个岗位收到的投递"`
	CandidateID uint64 `json:"candidate_id" jsonschema:"description=可选，限定只检索某个候选人的经历"`
	Limit       int    `json:"limit" jsonschema:"description=返回数量，默认使用系统配置，最大20"`
}

type ResumeSemanticSearchItem struct {
	Score           float32 `json:"score"`
	SemanticScore   float32 `json:"semantic_score,omitempty"`
	KeywordScore    float32 `json:"keyword_score,omitempty"`
	ChunkID         uint64  `json:"chunk_id"`
	ResumeID        uint64  `json:"resume_id"`
	JobID           uint64  `json:"job_id"`
	JobTitle        string  `json:"job_title"`
	CandidateID     uint64  `json:"candidate_id"`
	CandidateName   string  `json:"candidate_name"`
	SectionType     string  `json:"section_type"`
	SectionTitle    string  `json:"section_title"`
	ExperienceIndex int64   `json:"experience_index"`
	ChunkIndex      int64   `json:"chunk_index"`
	Content         string  `json:"content"`
	ResumeName      string  `json:"resume_name"`
}

type ResumeRecommendationInput struct {
	JobID          uint64   `json:"job_id" jsonschema:"description=可选，限定某个岗位；如果传入会读取该岗位 JD 作为推荐上下文"`
	JobDescription string   `json:"job_description" jsonschema:"description=可选，岗位 JD 或 HR 对候选人的要求；job_id 为空时必须提供"`
	Queries        []string `json:"queries" jsonschema:"description=HR 本次推荐的补充要求、偏好或限制，会和 JD 一起提炼 RAG 检索计划"`
	Limit          int      `json:"limit" jsonschema:"description=返回候选人数，默认10，最大10"`
	EvidenceLimit  int      `json:"evidence_limit" jsonschema:"description=每个候选人返回的证据片段数，默认3，最大5"`
	JobVersion     string   `json:"job_version,omitempty"`
}

type ResumeRecommendationOutput struct {
	Scope          string                          `json:"scope"`
	JobID          uint64                          `json:"job_id,omitempty"`
	JobTitle       string                          `json:"job_title,omitempty"`
	AgentStatus    string                          `json:"agent_status"`
	FallbackReason string                          `json:"fallback_reason,omitempty"`
	Candidates     []ResumeRecommendationCandidate `json:"candidates"`
}

type ResumeRecommendationCandidate struct {
	CandidateID   uint64                     `json:"candidate_id"`
	CandidateName string                     `json:"candidate_name"`
	ResumeID      uint64                     `json:"resume_id"`
	ResumeName    string                     `json:"resume_name"`
	JobID         uint64                     `json:"job_id"`
	JobTitle      string                     `json:"job_title"`
	Score         float32                    `json:"score"`
	SemanticScore float32                    `json:"semantic_score,omitempty"`
	KeywordScore  float32                    `json:"keyword_score,omitempty"`
	Reason        string                     `json:"reason,omitempty"`
	RiskPoints    []string                   `json:"risk_points,omitempty"`
	Evidence      []ResumeSemanticSearchItem `json:"evidence"`
}

func (c *Client) recommendResumesByJD(ctx context.Context, hrID uint64, input ResumeRecommendationInput, progress RecommendationProgressFunc) (ResumeRecommendationOutput, error) {
	if err := emitRecommendationProgress(progress, "preparing", "正在准备岗位和推荐条件"); err != nil {
		return ResumeRecommendationOutput{}, err
	}
	jobTitle := ""
	if input.JobID > 0 {
		var job domain.Job
		if err := c.db.WithContext(ctx).First(&job, "id = ? AND hr_id = ?", input.JobID, hrID).Error; err != nil {
			return ResumeRecommendationOutput{}, err
		}
		jobTitle = job.Title
		if input.JobDescription == "" {
			input.JobDescription = strings.Join([]string{job.Title, job.City, job.Education, job.Experience, job.Skills, job.Description}, "\n")
		}
	}
	if strings.TrimSpace(input.JobDescription) == "" && len(input.Queries) == 0 {
		return ResumeRecommendationOutput{}, fmt.Errorf("job_description or queries is required")
	}
	if err := emitRecommendationProgress(progress, "rag_initializing", "正在初始化 RAG 检索器"); err != nil {
		return ResumeRecommendationOutput{}, err
	}
	searcher, err := newResumeRAGSearcher(ctx, c.cfg.RAG)
	if err != nil {
		return ResumeRecommendationOutput{}, err
	}
	plan := c.parseRecommendationPlan(ctx, input, jobTitle, progress)
	if strings.TrimSpace(plan.VectorQuery) == "" && recommendationKeywordQuery(plan) == "" {
		return ResumeRecommendationOutput{}, fmt.Errorf("recommendation query is required")
	}
	if err := emitRecommendationProgress(progress, "rag_searching", "正在执行 Milvus hybrid RAG 检索"); err != nil {
		return ResumeRecommendationOutput{}, err
	}
	items, err := searcher.RecommendByPlan(ctx, hrID, input, plan, recommendationRecallTopK)
	if err != nil {
		return ResumeRecommendationOutput{}, err
	}
	if err := emitRecommendationProgress(progress, "evidence_loading", "正在回表补全候选人和经历证据"); err != nil {
		return ResumeRecommendationOutput{}, err
	}
	items, err = c.enrichResumeSemanticItems(ctx, hrID, 0, items)
	if err != nil {
		return ResumeRecommendationOutput{}, err
	}
	items = c.rerankRecommendationEvidence(ctx, input, jobTitle, plan, items, recommendationRerankTopK, progress)
	candidates := aggregateRecommendationCandidates(items, normalizeRecommendationLimit(input.Limit), normalizeEvidenceLimit(input.EvidenceLimit))
	if err := emitRecommendationProgress(progress, "agent_judging", "正在由推荐 agent 基于证据判断候选人"); err != nil {
		return ResumeRecommendationOutput{}, err
	}
	candidates, agentStatus, fallbackReason := c.judgeRecommendationCandidates(ctx, input, jobTitle, candidates, progress)
	if agentStatus == recommendationAgentStatusRAGOnly && fallbackReason != "" {
		if err := emitRecommendationProgress(progress, "agent_fallback", fallbackReason); err != nil {
			return ResumeRecommendationOutput{}, err
		}
	} else {
		if err := emitRecommendationProgress(progress, "agent_done", "推荐 agent 判断完成"); err != nil {
			return ResumeRecommendationOutput{}, err
		}
	}
	return ResumeRecommendationOutput{
		Scope:          "仅基于当前 HR 岗位收到的候选人自填项目/工作经历证据推荐候选人",
		JobID:          input.JobID,
		JobTitle:       jobTitle,
		AgentStatus:    agentStatus,
		FallbackReason: fallbackReason,
		Candidates:     candidates,
	}, nil
}

func (c *Client) recommendResumesRAGOnly(ctx context.Context, hrID uint64, input ResumeRecommendationInput, fallbackReason string) (ResumeRecommendationOutput, error) {
	jobTitle := ""
	if input.JobID > 0 {
		var job domain.Job
		if err := c.db.WithContext(ctx).First(&job, "id = ? AND hr_id = ?", input.JobID, hrID).Error; err != nil {
			return ResumeRecommendationOutput{}, err
		}
		jobTitle = job.Title
		if input.JobDescription == "" {
			input.JobDescription = strings.Join([]string{job.Title, job.City, job.Education, job.Experience, job.Skills, job.Description}, "\n")
		}
	}
	if strings.TrimSpace(input.JobDescription) == "" && len(input.Queries) == 0 {
		return ResumeRecommendationOutput{}, fmt.Errorf("job_description or queries is required")
	}
	searcher, err := newResumeRAGSearcher(ctx, c.cfg.RAG)
	if err != nil {
		return ResumeRecommendationOutput{}, err
	}
	plan := fallbackRecommendationParsePlan(input)
	if strings.TrimSpace(plan.VectorQuery) == "" && recommendationKeywordQuery(plan) == "" {
		return ResumeRecommendationOutput{}, fmt.Errorf("recommendation query is required")
	}
	items, err := searcher.RecommendByPlan(ctx, hrID, input, plan, recommendationRecallTopK)
	if err != nil {
		return ResumeRecommendationOutput{}, err
	}
	items, err = c.enrichResumeSemanticItems(ctx, hrID, 0, items)
	if err != nil {
		return ResumeRecommendationOutput{}, err
	}
	items = topRecommendationEvidence(items, recommendationRerankTopK)
	candidates := aggregateRecommendationCandidates(items, normalizeRecommendationLimit(input.Limit), normalizeEvidenceLimit(input.EvidenceLimit))
	if fallbackReason == "" {
		fallbackReason = "推荐任务多次重试失败，已返回 RAG 检索候选人兜底结果"
	}
	return ResumeRecommendationOutput{
		Scope:          "仅基于当前 HR 岗位收到的候选人自填项目/工作经历证据推荐候选人",
		JobID:          input.JobID,
		JobTitle:       jobTitle,
		AgentStatus:    recommendationAgentStatusRAGOnly,
		FallbackReason: fallbackReason,
		Candidates:     fallbackRecommendationCandidates(candidates),
	}, nil
}

func emitRecommendationProgress(progress RecommendationProgressFunc, stage string, message string) error {
	if progress == nil {
		return nil
	}
	return progress(stage, message)
}

func (c *Client) enrichResumeSemanticItems(ctx context.Context, hrID uint64, candidateID uint64, items []ResumeSemanticSearchItem) ([]ResumeSemanticSearchItem, error) {
	if len(items) == 0 {
		return items, nil
	}
	chunkIDs := make([]uint64, 0, len(items))
	seenChunks := map[uint64]struct{}{}
	for _, item := range items {
		if item.ChunkID == 0 {
			continue
		}
		if _, ok := seenChunks[item.ChunkID]; ok {
			continue
		}
		seenChunks[item.ChunkID] = struct{}{}
		chunkIDs = append(chunkIDs, item.ChunkID)
	}
	if len(chunkIDs) == 0 {
		return []ResumeSemanticSearchItem{}, nil
	}

	var chunkRows []domain.ResumeExperienceChunk
	query := c.db.WithContext(ctx).Where("id IN ? AND hr_id = ?", chunkIDs, hrID)
	if candidateID > 0 {
		query = query.Where("candidate_id = ?", candidateID)
	}
	if err := query.Find(&chunkRows).Error; err != nil {
		return nil, err
	}
	chunks := make(map[uint64]domain.ResumeExperienceChunk, len(chunkRows))
	jobIDs := make([]uint64, 0, len(chunkRows))
	candidateIDs := make([]uint64, 0, len(chunkRows))
	resumeIDs := make([]uint64, 0, len(chunkRows))
	seenJobs := map[uint64]struct{}{}
	seenCandidates := map[uint64]struct{}{}
	seenResumes := map[uint64]struct{}{}
	for _, chunk := range chunkRows {
		chunks[chunk.ID] = chunk
		if chunk.JobID > 0 {
			if _, ok := seenJobs[chunk.JobID]; !ok {
				seenJobs[chunk.JobID] = struct{}{}
				jobIDs = append(jobIDs, chunk.JobID)
			}
		}
		if chunk.CandidateID > 0 {
			if _, ok := seenCandidates[chunk.CandidateID]; !ok {
				seenCandidates[chunk.CandidateID] = struct{}{}
				candidateIDs = append(candidateIDs, chunk.CandidateID)
			}
		}
		if chunk.ResumeID > 0 {
			if _, ok := seenResumes[chunk.ResumeID]; !ok {
				seenResumes[chunk.ResumeID] = struct{}{}
				resumeIDs = append(resumeIDs, chunk.ResumeID)
			}
		}
	}

	jobs := map[uint64]domain.Job{}
	if len(jobIDs) > 0 {
		var rows []domain.Job
		if err := c.db.WithContext(ctx).Find(&rows, jobIDs).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			jobs[row.ID] = row
		}
	}
	profiles := map[uint64]domain.CandidateProfile{}
	if len(candidateIDs) > 0 {
		var rows []domain.CandidateProfile
		if err := c.db.WithContext(ctx).Where("user_id IN ?", candidateIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			profiles[row.UserID] = row
		}
	}
	resumes := map[uint64]domain.Resume{}
	if len(resumeIDs) > 0 {
		var rows []domain.Resume
		if err := c.db.WithContext(ctx).Find(&rows, resumeIDs).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			resumes[row.ID] = row
		}
	}

	enriched := make([]ResumeSemanticSearchItem, 0, len(items))
	for _, item := range items {
		chunk, ok := chunks[item.ChunkID]
		if !ok {
			continue
		}
		item.JobID = chunk.JobID
		item.ResumeID = chunk.ResumeID
		item.CandidateID = chunk.CandidateID
		item.SectionType = chunk.SectionType
		item.SectionTitle = chunk.SectionTitle
		item.ExperienceIndex = int64(chunk.ExperienceIndex)
		item.ChunkIndex = int64(chunk.ChunkIndex)
		item.Content = chunk.EvidenceText
		if job, ok := jobs[item.JobID]; ok {
			item.JobTitle = job.Title
		}
		if profile, ok := profiles[item.CandidateID]; ok {
			item.CandidateName = profile.Name
		}
		if resume, ok := resumes[item.ResumeID]; ok {
			item.ResumeName = resume.FileName
		}
		enriched = append(enriched, item)
	}
	return enriched, nil
}

func clampFloat32(value, minValue, maxValue float32) float32 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
