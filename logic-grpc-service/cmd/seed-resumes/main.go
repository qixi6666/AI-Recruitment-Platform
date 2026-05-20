package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"recruitment/logic-grpc-service/internal/ai"
	"recruitment/logic-grpc-service/internal/config"
	"recruitment/logic-grpc-service/internal/domain"
	"recruitment/logic-grpc-service/internal/repository"
)

const seedPassword = "Candidate@123"

type generatedResume struct {
	Name           string   `json:"name"`
	Phone          string   `json:"phone"`
	Education      string   `json:"education"`
	School         string   `json:"school"`
	Skills         []string `json:"skills"`
	Experience     string   `json:"experience"`
	ResumeMarkdown string   `json:"resume_markdown"`
}

func main() {
	var (
		count      = flag.Int("count", 50, "number of resumes to generate")
		startIndex = flag.Int("start-index", 14, "first candidate sequence number to generate")
		jobID      = flag.Uint64("job-id", 0, "existing job id to apply to; creates a seed HR/job when empty")
		outputDir  = flag.String("output-dir", defaultOutputDir(), "directory for generated resume files")
		indexRAG   = flag.Bool("index-rag", true, "index generated resumes into Milvus RAG after inserting MySQL records")
	)
	flag.Parse()
	if *count <= 0 {
		log.Fatalf("count must be greater than 0")
	}
	if *startIndex <= 0 {
		log.Fatalf("start-index must be greater than 0")
	}

	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	db, err := repository.Open(repository.Config{
		DSN:             cfg.MySQL.DSN,
		MaxOpenConns:    cfg.MySQL.MaxOpenConns,
		MaxIdleConns:    cfg.MySQL.MaxIdleConns,
		ConnMaxLifetime: time.Duration(cfg.MySQL.ConnMaxLifetimeSeconds) * time.Second,
	})
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}

	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		log.Fatalf("create output dir: %v", err)
	}

	job, hr, err := resolveSeedJob(ctx, db, *jobID)
	if err != nil {
		log.Fatalf("resolve job: %v", err)
	}

	generator, err := newResumeGenerator(ctx, cfg.AI)
	if err != nil {
		log.Fatalf("create resume generator: %v", err)
	}
	aiClient := ai.NewClient(cfg.AI, db)

	endIndex := *startIndex + *count - 1
	for seq := *startIndex; seq <= endIndex; seq++ {
		resume, err := generator.Generate(ctx, seq, job)
		if err != nil {
			log.Fatalf("generate resume %d: %v", seq, err)
		}
		candidate, profile, resumeRow, app, err := upsertCandidateApplication(ctx, db, seq, resume, job, *outputDir)
		if err != nil {
			log.Fatalf("insert resume %d: %v", seq, err)
		}
		if *indexRAG && cfg.AI.RAG.Enabled {
			if err := aiClient.IndexApplicationResume(ctx, hr.ID, job.ID, app.ID, candidate.ID, resumeRow, profile, job.Title); err != nil {
				log.Printf("index RAG failed: candidate=%s resume_id=%d err=%v", candidate.Username, resumeRow.ID, err)
			}
		}
		log.Printf("seeded %02d/%02d candidate=%s resume=%s", seq-*startIndex+1, *count, candidate.Username, resumeRow.FileName)
	}
	log.Printf("done. HR username=%s password=%s job_id=%d", hr.Username, seedPassword, job.ID)
}

type resumeGenerator struct {
	model *einoopenai.ChatModel
}

func newResumeGenerator(ctx context.Context, cfg config.AIConfig) (*resumeGenerator, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY or OPENAI_API_KEY is required")
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	temp := float32(0.85)
	maxTokens := 2200
	model, err := einoopenai.NewChatModel(ctx, &einoopenai.ChatModelConfig{
		APIKey:      cfg.APIKey,
		BaseURL:     cfg.BaseURL,
		Model:       cfg.Model,
		Timeout:     timeout,
		MaxTokens:   &maxTokens,
		Temperature: &temp,
	})
	if err != nil {
		return nil, err
	}
	return &resumeGenerator{model: model}, nil
}

func (g *resumeGenerator) Generate(ctx context.Context, idx int, job domain.Job) (generatedResume, error) {
	prompt := fmt.Sprintf(`生成一份中文技术候选人简历测试数据，目标岗位如下：
岗位：%s
城市：%s
技能：%s
描述：%s

要求：
1. 只输出 JSON，不要 Markdown 代码块。
2. JSON 字段必须是 name, phone, education, school, skills, experience, resume_markdown。
3. skills 是字符串数组，其他字段是字符串。
4. resume_markdown 必须包含“项目经历”“实习经历”或“工作经历”标题，且要有真实感强的项目细节、技术栈、职责、结果。
5. 候选人可以分布在 Go、Java、Python、前端、测试、数据、算法、运维等方向，和岗位匹配度要有高有低，便于推荐排序。
6. 不要生成真实身份证、真实邮箱、真实公司敏感信息。手机号用 13/15/18 开头的虚构号码。
7. 这是第 %d 份简历，内容要和其他候选人明显不同。`, job.Title, job.City, job.Skills, job.Description, idx)

	resp, err := g.model.Generate(ctx, []*schema.Message{
		schema.SystemMessage("你是招聘系统的测试数据生成器，必须输出可解析 JSON。"),
		schema.UserMessage(prompt),
	})
	if err != nil {
		return generatedResume{}, err
	}
	var out generatedResume
	if err := json.Unmarshal([]byte(extractJSONObject(resp.Content)), &out); err != nil {
		return generatedResume{}, fmt.Errorf("parse model JSON: %w; content=%s", err, resp.Content)
	}
	if strings.TrimSpace(out.Name) == "" || strings.TrimSpace(out.ResumeMarkdown) == "" {
		return generatedResume{}, fmt.Errorf("model returned incomplete resume")
	}
	return out, nil
}

func resolveSeedJob(ctx context.Context, db *gorm.DB, requestedJobID uint64) (domain.Job, domain.User, error) {
	if requestedJobID > 0 {
		var job domain.Job
		if err := db.WithContext(ctx).First(&job, "id = ?", requestedJobID).Error; err != nil {
			return domain.Job{}, domain.User{}, err
		}
		var hr domain.User
		if err := db.WithContext(ctx).First(&hr, "id = ?", job.HRID).Error; err != nil {
			return domain.Job{}, domain.User{}, err
		}
		return job, hr, nil
	}

	hr, err := upsertUser(ctx, db, "seed_hr", domain.RoleHR)
	if err != nil {
		return domain.Job{}, domain.User{}, err
	}
	job := domain.Job{
		HRID:        hr.ID,
		Title:       "Go 后端工程师",
		City:        "上海",
		SalaryMin:   18000,
		SalaryMax:   35000,
		Education:   "本科及以上",
		Experience:  "1-5年",
		Skills:      "Go,gRPC,MySQL,Redis,消息队列,微服务,RAG",
		Description: "负责招聘业务平台后端服务建设，参与高并发接口、数据检索、AI RAG 简历推荐、服务治理和性能优化。",
		Status:      domain.JobStatusOpen,
	}
	err = db.WithContext(ctx).Where("hr_id = ? AND title = ?", hr.ID, job.Title).FirstOrCreate(&job).Error
	return job, hr, err
}

func upsertCandidateApplication(ctx context.Context, db *gorm.DB, idx int, data generatedResume, job domain.Job, outputDir string) (domain.User, domain.CandidateProfile, domain.Resume, domain.Application, error) {
	username := fmt.Sprintf("seed_candidate_%03d", idx)
	candidate, err := upsertUser(ctx, db, username, domain.RoleCandidate)
	if err != nil {
		return domain.User{}, domain.CandidateProfile{}, domain.Resume{}, domain.Application{}, err
	}

	profile := domain.CandidateProfile{
		UserID:            candidate.ID,
		Name:              strings.TrimSpace(data.Name),
		Phone:             strings.TrimSpace(data.Phone),
		Education:         strings.TrimSpace(data.Education),
		School:            strings.TrimSpace(data.School),
		Experience:        strings.TrimSpace(data.Experience),
		WorkExperience:    strings.TrimSpace(data.Experience),
		ProjectExperience: strings.TrimSpace(data.ResumeMarkdown),
		Skills:            strings.Join(cleanStrings(data.Skills), ","),
	}
	if profile.Phone == "" {
		profile.Phone = fmt.Sprintf("138%08d", idx)
	}
	if profile.Education == "" {
		profile.Education = "本科"
	}
	if profile.School == "" {
		profile.School = "某某大学"
	}
	if profile.Experience == "" {
		profile.Experience = data.ResumeMarkdown
	}
	if profile.WorkExperience == "" {
		profile.WorkExperience = profile.Experience
	}
	if profile.ProjectExperience == "" {
		profile.ProjectExperience = data.ResumeMarkdown
	}
	if profile.Skills == "" {
		profile.Skills = "Go,MySQL,Redis"
	}
	if err := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "phone", "education", "school", "experience", "work_experience", "project_experience", "skills", "updated_at"}),
	}).Create(&profile).Error; err != nil {
		return domain.User{}, domain.CandidateProfile{}, domain.Resume{}, domain.Application{}, err
	}

	fileName := fmt.Sprintf("%s_resume_%03d.doc", username, idx)
	filePath := filepath.Join(outputDir, fileName)
	content := normalizeResumeDocument(data.ResumeMarkdown, profile)
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return domain.User{}, domain.CandidateProfile{}, domain.Resume{}, domain.Application{}, err
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return domain.User{}, domain.CandidateProfile{}, domain.Resume{}, domain.Application{}, err
	}

	resume := domain.Resume{
		UserID:      candidate.ID,
		FileName:    fileName,
		ObjectKey:   filePath,
		ContentType: "application/msword",
		Size:        info.Size(),
		Status:      domain.ResumeReady,
	}
	if err := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "object_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"file_name", "content_type", "size", "status", "updated_at"}),
	}).Create(&resume).Error; err != nil {
		return domain.User{}, domain.CandidateProfile{}, domain.Resume{}, domain.Application{}, err
	}
	if err := db.WithContext(ctx).First(&resume, "object_key = ?", filePath).Error; err != nil {
		return domain.User{}, domain.CandidateProfile{}, domain.Resume{}, domain.Application{}, err
	}

	app := domain.Application{JobID: job.ID, UserID: candidate.ID, ResumeID: resume.ID, Status: domain.ApplicationSubmitted}
	if err := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "job_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"resume_id", "status", "updated_at"}),
	}).Create(&app).Error; err != nil {
		return domain.User{}, domain.CandidateProfile{}, domain.Resume{}, domain.Application{}, err
	}
	if err := db.WithContext(ctx).First(&app, "job_id = ? AND user_id = ?", job.ID, candidate.ID).Error; err != nil {
		return domain.User{}, domain.CandidateProfile{}, domain.Resume{}, domain.Application{}, err
	}
	return candidate, profile, resume, app, nil
}

func upsertUser(ctx context.Context, db *gorm.DB, username, role string) (domain.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(seedPassword), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, err
	}
	user := domain.User{Username: username, Role: role, PasswordHash: string(hash)}
	err = db.WithContext(ctx).Where("username = ? AND role = ?", username, role).FirstOrCreate(&user).Error
	return user, err
}

func normalizeResumeDocument(markdown string, profile domain.CandidateProfile) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(profile.Name)
	b.WriteString("\n\n")
	b.WriteString("最高学历：")
	b.WriteString(profile.Education)
	b.WriteString("\n毕业院校：")
	b.WriteString(profile.School)
	b.WriteString("\n核心技能：")
	b.WriteString(profile.Skills)
	b.WriteString("\n\n")
	if strings.TrimSpace(profile.WorkExperience) != "" {
		b.WriteString("## 工作经历\n\n")
		b.WriteString(strings.TrimSpace(profile.WorkExperience))
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(profile.ProjectExperience) != "" {
		b.WriteString("## 项目经历\n\n")
		b.WriteString(strings.TrimSpace(profile.ProjectExperience))
	} else {
		b.WriteString(strings.TrimSpace(markdown))
	}
	b.WriteString("\n")
	return b.String()
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func extractJSONObject(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	value = strings.TrimSpace(value)
	start := strings.Index(value, "{")
	end := strings.LastIndex(value, "}")
	if start >= 0 && end > start {
		return value[start : end+1]
	}
	return value
}

func defaultOutputDir() string {
	if dir := strings.TrimSpace(os.Getenv("UPLOAD_DIR")); dir != "" {
		return filepath.Join(dir, "seed-resumes")
	}
	return filepath.Join("..", "uploads", "seed-resumes")
}
