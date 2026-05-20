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

type OverallStatsInput struct {
	Status string `json:"status" jsonschema:"description=岗位状态过滤，只能是 open 或 offline；不确定时留空统计全部"`
}

type OverallStatsOutput struct {
	Scope              string `json:"scope"`
	JobCount           int64  `json:"job_count"`
	OpenJobCount       int64  `json:"open_job_count"`
	OfflineJobCount    int64  `json:"offline_job_count"`
	ApplicationCount   int64  `json:"application_count"`
	UniqueCandidateNum int64  `json:"unique_candidate_num"`
}

type JobHotnessInput struct {
	JobFilters
	Limit int `json:"limit" jsonschema:"description=返回数量，默认10，最大50"`
}

type JobHotnessOutput struct {
	Items []JobHotnessItem `json:"items"`
}

type JobHotnessItem struct {
	JobID            uint64 `json:"job_id"`
	Title            string `json:"title"`
	City             string `json:"city"`
	Status           string `json:"status"`
	ApplicationCount int64  `json:"application_count"`
}

type JobApplicationStatsInput struct {
	JobFilters
	Limit int `json:"limit" jsonschema:"description=返回数量，默认10，最大50"`
}

type JobApplicationStatsOutput struct {
	Items []JobApplicationStatsItem `json:"items"`
}

type JobApplicationStatsItem struct {
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
	Name       string `json:"name"`
	Education  string `json:"education"`
	School     string `json:"school"`
	Skills     string `json:"skills"`
	JobID      uint64 `json:"job_id"`
	JobTitle   string `json:"job_title"`
	AppliedAt  string `json:"applied_at"`
	ResumeName string `json:"resume_name"`
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
	Evidence      []ResumeSemanticSearchItem `json:"evidence"`
}

func (c *Client) toolsForHR(hrID uint64) []tool.BaseTool {
	tools := make([]tool.BaseTool, 0, 6)
	must := func(t tool.InvokableTool, err error) {
		if err != nil {
			panic(err)
		}
		tools = append(tools, t)
	}
	must(utils.InferTool(
		"get_overall_recruitment_stats",
		"查询当前 HR 的招聘总览统计，包括岗位数、开放岗位数、下架岗位数、投递总数、去重候选人数。适合回答投递总人数、整体概览类问题。",
		c.getOverallStatsTool(hrID),
	))
	must(utils.InferTool(
		"get_job_hotness_rank",
		"查询当前 HR 岗位热度排行，按投递数量排序。适合回答哪个岗位最热门、哪些岗位投递最多、岗位热度数据。",
		c.getJobHotnessTool(hrID),
	))
	must(utils.InferTool(
		"get_job_application_stats",
		"查询当前 HR 单岗位或多岗位投递统计，可按岗位关键词、城市、状态、技能过滤。适合回答某个岗位有多少投递。",
		c.getJobApplicationStatsTool(hrID),
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

func (c *Client) getOverallStatsTool(hrID uint64) func(context.Context, OverallStatsInput) (OverallStatsOutput, error) {
	return func(ctx context.Context, input OverallStatsInput) (OverallStatsOutput, error) {
		status := validStatus(input.Status)
		db := c.db.WithContext(ctx)
		jobQ := db.Model(&domain.Job{}).Where("hr_id = ?", hrID)
		if status != "" {
			jobQ = jobQ.Where("status = ?", status)
		}
		var jobCount, openJobCount, offlineJobCount, appCount, candidateCount int64
		if err := jobQ.Count(&jobCount).Error; err != nil {
			return OverallStatsOutput{}, err
		}
		if err := db.Model(&domain.Job{}).Where("hr_id = ? AND status = ?", hrID, domain.JobStatusOpen).Count(&openJobCount).Error; err != nil {
			return OverallStatsOutput{}, err
		}
		if err := db.Model(&domain.Job{}).Where("hr_id = ? AND status = ?", hrID, domain.JobStatusOffline).Count(&offlineJobCount).Error; err != nil {
			return OverallStatsOutput{}, err
		}
		appQ := db.Model(&domain.Application{}).Joins("JOIN jobs ON jobs.id = applications.job_id AND jobs.hr_id = ?", hrID)
		if status != "" {
			appQ = appQ.Where("jobs.status = ?", status)
		}
		if err := appQ.Count(&appCount).Error; err != nil {
			return OverallStatsOutput{}, err
		}
		if err := appQ.Distinct("applications.user_id").Count(&candidateCount).Error; err != nil {
			return OverallStatsOutput{}, err
		}
		return OverallStatsOutput{
			Scope:              "仅统计当前 HR 创建岗位下的数据",
			JobCount:           jobCount,
			OpenJobCount:       openJobCount,
			OfflineJobCount:    offlineJobCount,
			ApplicationCount:   appCount,
			UniqueCandidateNum: candidateCount,
		}, nil
	}
}

func (c *Client) getJobHotnessTool(hrID uint64) func(context.Context, JobHotnessInput) (JobHotnessOutput, error) {
	return func(ctx context.Context, input JobHotnessInput) (JobHotnessOutput, error) {
		var items []JobHotnessItem
		q := c.db.WithContext(ctx).Table("applications").
			Select("jobs.id AS job_id, jobs.title, jobs.city, jobs.status, COUNT(applications.id) AS application_count").
			Joins("JOIN jobs ON jobs.id = applications.job_id").
			Where("jobs.hr_id = ?", hrID)
		q = applyJobFilters(q, input.JobFilters, true)
		err := q.Group("jobs.id, jobs.title, jobs.city, jobs.status").
			Order("application_count DESC").
			Limit(normalizeLimit(input.Limit)).
			Scan(&items).Error
		return JobHotnessOutput{Items: items}, err
	}
}

func (c *Client) getJobApplicationStatsTool(hrID uint64) func(context.Context, JobApplicationStatsInput) (JobApplicationStatsOutput, error) {
	return func(ctx context.Context, input JobApplicationStatsInput) (JobApplicationStatsOutput, error) {
		var items []JobApplicationStatsItem
		q := c.db.WithContext(ctx).Table("jobs").
			Select("jobs.id AS job_id, jobs.title, jobs.city, jobs.status, COUNT(applications.id) AS application_count, COUNT(DISTINCT applications.user_id) AS unique_candidate_count").
			Joins("LEFT JOIN applications ON applications.job_id = jobs.id").
			Where("jobs.hr_id = ?", hrID)
		q = applyJobFilters(q, input.JobFilters, true)
		err := q.Group("jobs.id, jobs.title, jobs.city, jobs.status").
			Order("application_count DESC, jobs.created_at DESC").
			Limit(normalizeLimit(input.Limit)).
			Scan(&items).Error
		return JobApplicationStatsOutput{Items: items}, err
	}
}

func (c *Client) searchCandidatesTool(hrID uint64) func(context.Context, CandidateSearchInput) (CandidateSearchOutput, error) {
	return func(ctx context.Context, input CandidateSearchInput) (CandidateSearchOutput, error) {
		type candidateRow struct {
			Name       string
			Education  string
			School     string
			Skills     string
			JobID      uint64
			JobTitle   string
			AppliedAt  time.Time
			ResumeName string
		}
		var rows []candidateRow
		q := c.db.WithContext(ctx).Table("applications").
			Select("candidate_profiles.name, candidate_profiles.education, candidate_profiles.school, candidate_profiles.skills, jobs.id AS job_id, jobs.title AS job_title, applications.created_at AS applied_at, resumes.file_name AS resume_name").
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
				Name: row.Name, Education: row.Education, School: row.School, Skills: row.Skills,
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
		if err := c.enrichResumeSemanticItems(ctx, items); err != nil {
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
		candidates, err := searcher.RecommendByJD(ctx, hrID, input)
		if err != nil {
			return ResumeRecommendationOutput{}, err
		}
		return ResumeRecommendationOutput{
			Scope:      "仅基于当前 HR 岗位收到的候选人自填项目/工作经历证据推荐候选人",
			JobID:      input.JobID,
			JobTitle:   jobTitle,
			Candidates: candidates,
		}, nil
	}
}

func (c *Client) enrichResumeSemanticItems(ctx context.Context, items []ResumeSemanticSearchItem) error {
	if len(items) == 0 {
		return nil
	}
	jobIDs := make([]uint64, 0, len(items))
	candidateIDs := make([]uint64, 0, len(items))
	resumeIDs := make([]uint64, 0, len(items))
	seenJobs := map[uint64]struct{}{}
	seenCandidates := map[uint64]struct{}{}
	seenResumes := map[uint64]struct{}{}
	for _, item := range items {
		if item.JobID > 0 {
			if _, ok := seenJobs[item.JobID]; !ok {
				seenJobs[item.JobID] = struct{}{}
				jobIDs = append(jobIDs, item.JobID)
			}
		}
		if item.CandidateID > 0 {
			if _, ok := seenCandidates[item.CandidateID]; !ok {
				seenCandidates[item.CandidateID] = struct{}{}
				candidateIDs = append(candidateIDs, item.CandidateID)
			}
		}
		if item.ResumeID > 0 {
			if _, ok := seenResumes[item.ResumeID]; !ok {
				seenResumes[item.ResumeID] = struct{}{}
				resumeIDs = append(resumeIDs, item.ResumeID)
			}
		}
	}

	jobs := map[uint64]domain.Job{}
	if len(jobIDs) > 0 {
		var rows []domain.Job
		if err := c.db.WithContext(ctx).Find(&rows, jobIDs).Error; err != nil {
			return err
		}
		for _, row := range rows {
			jobs[row.ID] = row
		}
	}
	profiles := map[uint64]domain.CandidateProfile{}
	if len(candidateIDs) > 0 {
		var rows []domain.CandidateProfile
		if err := c.db.WithContext(ctx).Where("user_id IN ?", candidateIDs).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			profiles[row.UserID] = row
		}
	}
	resumes := map[uint64]domain.Resume{}
	if len(resumeIDs) > 0 {
		var rows []domain.Resume
		if err := c.db.WithContext(ctx).Find(&rows, resumeIDs).Error; err != nil {
			return err
		}
		for _, row := range rows {
			resumes[row.ID] = row
		}
	}

	for i := range items {
		if items[i].JobTitle == "" {
			if job, ok := jobs[items[i].JobID]; ok {
				items[i].JobTitle = job.Title
			}
		}
		if items[i].CandidateName == "" {
			if profile, ok := profiles[items[i].CandidateID]; ok {
				items[i].CandidateName = profile.Name
			}
		}
		if items[i].ResumeName == "" {
			if resume, ok := resumes[items[i].ResumeID]; ok {
				items[i].ResumeName = resume.FileName
			}
		}
	}
	return nil
}
