package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"recruitment/logic-grpc-service/internal/domain"
	"recruitment/shared/rpc"
)

type JobFilters struct {
	JobKeywords []string `json:"job_keywords" jsonschema:"description=岗位关键词，可从岗位名称、技能或描述中提取，例如 Go 后端、产品经理"`
	Skills      []string `json:"skills" jsonschema:"description=技能标签，例如 Go、MySQL、gRPC、React"`
	City        string   `json:"city" jsonschema:"description=岗位城市，例如 上海、北京、杭州"`
	Status      string   `json:"status" jsonschema:"description=岗位状态，只能是 open 或 offline；不确定时留空"`
}

type RecruitmentStatsInput struct {
	JobFilters
	Scope string `json:"scope" jsonschema:"description=统计范围，overall 返回总体概览，by_job 返回岗位投递明细/热度排行，all 同时返回；不确定时用 all"`
	Limit int    `json:"limit" jsonschema:"description=岗位明细返回数量，默认10，最大50"`
}

type RecruitmentStatsOutput struct {
	Scope   string                    `json:"scope"`
	Overall *RecruitmentOverallStats  `json:"overall,omitempty"`
	Jobs    []RecruitmentJobStatsItem `json:"jobs,omitempty"`
}

type RecruitmentOverallStats struct {
	JobCount           int64 `json:"job_count"`
	OpenJobCount       int64 `json:"open_job_count"`
	OfflineJobCount    int64 `json:"offline_job_count"`
	ApplicationCount   int64 `json:"application_count"`
	UniqueCandidateNum int64 `json:"unique_candidate_num"`
}

type RecruitmentJobStatsItem struct {
	JobID                uint64 `json:"job_id"`
	Title                string `json:"title"`
	City                 string `json:"city"`
	Status               string `json:"status"`
	ApplicationCount     int64  `json:"application_count"`
	UniqueCandidateCount int64  `json:"unique_candidate_count"`
}

type CandidateSearchInput struct {
	JobFilters
	Education string `json:"education" jsonschema:"description=候选人最高学历，例如 本科、硕士、博士"`
	School    string `json:"school" jsonschema:"description=毕业院校关键词"`
	Limit     int    `json:"limit" jsonschema:"description=返回数量，默认10，最大50"`
}

type CandidateSearchOutput struct {
	Items []CandidateSearchItem `json:"items"`
}

type CandidateSearchItem struct {
	CandidateID   uint64 `json:"candidate_id"`
	ApplicationID uint64 `json:"application_id"`
	Name          string `json:"name"`
	Education     string `json:"education"`
	School        string `json:"school"`
	Skills        string `json:"skills"`
	JobID         uint64 `json:"job_id"`
	JobTitle      string `json:"job_title"`
	AppliedAt     string `json:"applied_at"`
	ResumeName    string `json:"resume_name"`
}

type ResumeSemanticSearchInput struct {
	Query       string `json:"query" jsonschema:"description=用于语义检索候选人填写的项目/工作经历，例如 高并发订单系统经验、支付系统、gRPC 微服务治理"`
	JobID       uint64 `json:"job_id" jsonschema:"description=可选，限定只检索某个岗位收到的投递"`
	CandidateID uint64 `json:"candidate_id" jsonschema:"description=可选，限定只检索某个候选人的经历"`
	Limit       int    `json:"limit" jsonschema:"description=返回数量，默认使用系统配置，最大20"`
}

type ResumeSemanticSearchOutput struct {
	Scope string                     `json:"scope"`
	Items []ResumeSemanticSearchItem `json:"items"`
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
	Queries        []string `json:"queries" jsonschema:"description=由模型根据 JD 生成的多路检索 query，例如 Go 微服务项目经验、高并发系统优化、RAG 向量检索经验"`
	Limit          int      `json:"limit" jsonschema:"description=返回候选人数，默认5，最大10"`
	EvidenceLimit  int      `json:"evidence_limit" jsonschema:"description=每个候选人返回的证据片段数，默认3，最大5"`
}

type ResumeRecommendationOutput struct {
	Scope      string                          `json:"scope"`
	JobID      uint64                          `json:"job_id,omitempty"`
	JobTitle   string                          `json:"job_title,omitempty"`
	Candidates []ResumeRecommendationCandidate `json:"candidates"`
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
	Evidence      []ResumeSemanticSearchItem `json:"evidence"`
}

func (c *Client) toolsForHR(hrID uint64) []tool.BaseTool {
	tools := make([]tool.BaseTool, 0, 4)
	must := func(t tool.InvokableTool, err error) {
		if err != nil {
			panic(err)
		}
		tools = append(tools, t)
	}
	must(utils.InferTool(
		"get_recruitment_stats",
		"查询当前 HR 的招聘统计。scope=overall 返回岗位数、开放/下架岗位数、投递总数、去重候选人数；scope=by_job 返回岗位投递明细和热度排行；scope=all 同时返回。可按岗位关键词、技能、城市、状态过滤。",
		c.getRecruitmentStatsTool(hrID),
	))
	must(utils.InferTool(
		"search_candidates",
		"筛选当前 HR 岗位下的候选人，可按技能、学历、学校、城市、岗位关键词过滤。适合回答符合条件候选人筛选。",
		c.searchCandidatesTool(hrID),
	))
	if c.ragEnabled() {
		must(utils.InferTool(
			"semantic_search_resumes",
			"对当前 HR 岗位收到的候选人自填项目/工作经历进行 Milvus Hybrid RAG 检索，适合查找经历证据片段。",
			c.semanticSearchResumesTool(hrID),
		))
		must(utils.InferTool(
			"recommend_resumes_by_jd",
			"根据岗位 JD 推荐当前 HR 岗位下的候选人。工具会用 Milvus dense + BM25 hybrid 检索候选人自填项目/工作经历证据，按候选人聚合后返回证据片段；最终匹配度、评分和推荐理由由你基于证据判断。",
			c.recommendResumesByJDTool(hrID),
		))
	}
	return tools
}

func (c *Client) getRecruitmentStatsTool(hrID uint64) func(context.Context, RecruitmentStatsInput) (RecruitmentStatsOutput, error) {
	return func(ctx context.Context, input RecruitmentStatsInput) (RecruitmentStatsOutput, error) {
		scope := normalizeRecruitmentStatsScope(input.Scope)
		output := RecruitmentStatsOutput{Scope: "仅统计当前 HR 创建岗位下的数据"}
		if scope == "overall" || scope == "all" {
			overall, err := c.recruitmentOverallStats(ctx, hrID, input.JobFilters)
			if err != nil {
				return RecruitmentStatsOutput{}, err
			}
			output.Overall = &overall
		}
		if scope == "by_job" || scope == "all" {
			jobs, err := c.recruitmentJobStats(ctx, hrID, input.JobFilters, input.Limit)
			if err != nil {
				return RecruitmentStatsOutput{}, err
			}
			output.Jobs = jobs
		}
		return output, nil
	}
}

func (c *Client) recruitmentOverallStats(ctx context.Context, hrID uint64, filters JobFilters) (RecruitmentOverallStats, error) {
	db := c.db.WithContext(ctx)
	jobQ := applyJobFilters(db.Model(&domain.Job{}).Where("jobs.hr_id = ?", hrID), filters, true)

	var stats RecruitmentOverallStats
	if err := jobQ.Count(&stats.JobCount).Error; err != nil {
		return RecruitmentOverallStats{}, err
	}
	if err := c.countJobsByStatus(ctx, hrID, filters, domain.JobStatusOpen, &stats.OpenJobCount); err != nil {
		return RecruitmentOverallStats{}, err
	}
	if err := c.countJobsByStatus(ctx, hrID, filters, domain.JobStatusOffline, &stats.OfflineJobCount); err != nil {
		return RecruitmentOverallStats{}, err
	}

	appQ := db.Model(&domain.Application{}).
		Joins("JOIN jobs ON jobs.id = applications.job_id AND jobs.hr_id = ?", hrID)
	appQ = applyJobFilters(appQ, filters, true)
	if err := appQ.Count(&stats.ApplicationCount).Error; err != nil {
		return RecruitmentOverallStats{}, err
	}
	if err := appQ.Distinct("applications.user_id").Count(&stats.UniqueCandidateNum).Error; err != nil {
		return RecruitmentOverallStats{}, err
	}
	return stats, nil
}

func (c *Client) countJobsByStatus(ctx context.Context, hrID uint64, filters JobFilters, status string, count *int64) error {
	filters.Status = status
	q := c.db.WithContext(ctx).Model(&domain.Job{}).Where("jobs.hr_id = ?", hrID)
	q = applyJobFilters(q, filters, true)
	return q.Count(count).Error
}

func (c *Client) recruitmentJobStats(ctx context.Context, hrID uint64, filters JobFilters, limit int) ([]RecruitmentJobStatsItem, error) {
	var items []RecruitmentJobStatsItem
	q := c.db.WithContext(ctx).Table("jobs").
		Select("jobs.id AS job_id, jobs.title, jobs.city, jobs.status, COUNT(applications.id) AS application_count, COUNT(DISTINCT applications.user_id) AS unique_candidate_count").
		Joins("LEFT JOIN applications ON applications.job_id = jobs.id").
		Where("jobs.hr_id = ?", hrID)
	q = applyJobFilters(q, filters, true)
	err := q.Group("jobs.id, jobs.title, jobs.city, jobs.status").
		Order("application_count DESC, jobs.created_at DESC").
		Limit(normalizeLimit(limit)).
		Scan(&items).Error
	return items, err
}

func normalizeRecruitmentStatsScope(scope string) string {
	scope = strings.ToLower(cleanText(scope))
	switch scope {
	case "overall", "by_job", "all":
		return scope
	default:
		return "all"
	}
}

func (c *Client) searchCandidatesTool(hrID uint64) func(context.Context, CandidateSearchInput) (CandidateSearchOutput, error) {
	return func(ctx context.Context, input CandidateSearchInput) (CandidateSearchOutput, error) {
		type candidateRow struct {
			CandidateID   uint64
			ApplicationID uint64
			Name          string
			Education     string
			School        string
			Skills        string
			JobID         uint64
			JobTitle      string
			AppliedAt     time.Time
			ResumeName    string
		}
		var rows []candidateRow
		q := c.db.WithContext(ctx).Table("applications").
			Select("applications.user_id AS candidate_id, applications.id AS application_id, candidate_profiles.name, candidate_profiles.education, candidate_profiles.school, candidate_profiles.skills, jobs.id AS job_id, jobs.title AS job_title, applications.created_at AS applied_at, resumes.file_name AS resume_name").
			Joins("JOIN jobs ON jobs.id = applications.job_id").
			Joins("JOIN candidate_profiles ON candidate_profiles.user_id = applications.user_id").
			Joins("JOIN resumes ON resumes.id = applications.resume_id").
			Where("jobs.hr_id = ?", hrID)
		q = applyJobFilters(q, input.JobFilters, false)
		for _, skill := range cleanList(input.Skills, 8) {
			q = q.Where("candidate_profiles.skills LIKE ?", "%"+skill+"%")
		}
		if input.Education != "" {
			q = q.Where("candidate_profiles.education LIKE ?", "%"+cleanText(input.Education)+"%")
		}
		if input.School != "" {
			q = q.Where("candidate_profiles.school LIKE ?", "%"+cleanText(input.School)+"%")
		}
		err := q.Order("applications.created_at DESC").
			Limit(normalizeLimit(input.Limit)).
			Scan(&rows).Error
		if err != nil {
			return CandidateSearchOutput{}, err
		}
		items := make([]CandidateSearchItem, 0, len(rows))
		for _, row := range rows {
			items = append(items, CandidateSearchItem{
				CandidateID: row.CandidateID, ApplicationID: row.ApplicationID, Name: row.Name, Education: row.Education, School: row.School, Skills: row.Skills,
				JobID: row.JobID, JobTitle: row.JobTitle, AppliedAt: rpc.FormatTime(row.AppliedAt), ResumeName: row.ResumeName,
			})
		}
		return CandidateSearchOutput{Items: items}, nil
	}
}

func (c *Client) semanticSearchResumesTool(hrID uint64) func(context.Context, ResumeSemanticSearchInput) (ResumeSemanticSearchOutput, error) {
	return func(ctx context.Context, input ResumeSemanticSearchInput) (ResumeSemanticSearchOutput, error) {
		searcher, err := newResumeRAGSearcher(ctx, c.cfg.RAG)
		if err != nil {
			return ResumeSemanticSearchOutput{}, err
		}
		items, err := searcher.Search(ctx, hrID, input)
		if err != nil {
			return ResumeSemanticSearchOutput{}, err
		}
		items, err = c.enrichResumeSemanticItems(ctx, hrID, input.CandidateID, items)
		if err != nil {
			return ResumeSemanticSearchOutput{}, err
		}
		return ResumeSemanticSearchOutput{
			Scope: "仅检索当前 HR 创建岗位收到的候选人自填经历向量片段",
			Items: items,
		}, nil
	}
}

func (c *Client) recommendResumesByJDTool(hrID uint64) func(context.Context, ResumeRecommendationInput) (ResumeRecommendationOutput, error) {
	return func(ctx context.Context, input ResumeRecommendationInput) (ResumeRecommendationOutput, error) {
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
		items, err := searcher.RecommendByJD(ctx, hrID, input)
		if err != nil {
			return ResumeRecommendationOutput{}, err
		}
		items, err = c.enrichResumeSemanticItems(ctx, hrID, 0, items)
		if err != nil {
			return ResumeRecommendationOutput{}, err
		}
		candidates := aggregateRecommendationCandidates(items, normalizeRecommendationLimit(input.Limit), normalizeEvidenceLimit(input.EvidenceLimit))
		return ResumeRecommendationOutput{
			Scope:      "仅基于当前 HR 岗位收到的候选人自填项目/工作经历证据推荐候选人",
			JobID:      input.JobID,
			JobTitle:   jobTitle,
			Candidates: candidates,
		}, nil
	}
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
