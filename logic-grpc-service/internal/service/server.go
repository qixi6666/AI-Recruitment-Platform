package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"recruitment/logic-grpc-service/internal/ai"
	"recruitment/logic-grpc-service/internal/domain"
	"recruitment/shared/rpc"
)

type Server struct {
	db                  *gorm.DB
	ai                  *ai.Client
	memoryRounds        int
	memoryTriggerTokens int
	showToolResults     bool
}

type Option func(*Server)

func WithMemoryRounds(rounds int) Option {
	return func(s *Server) {
		if rounds > 0 {
			if rounds > 20 {
				rounds = 20
			}
			s.memoryRounds = rounds
		}
	}
}

func WithShowToolResults(show bool) Option {
	return func(s *Server) {
		s.showToolResults = show
	}
}

func WithMemoryTriggerTokens(tokens int) Option {
	return func(s *Server) {
		if tokens > 0 {
			s.memoryTriggerTokens = tokens
		}
	}
}

func NewServer(db *gorm.DB, aiClient *ai.Client, opts ...Option) *Server {
	s := &Server{db: db, ai: aiClient, memoryRounds: 5, memoryTriggerTokens: 6000}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Server) Register(ctx context.Context, req *rpc.RegisterRequest) (*rpc.RegisterResponse, error) {
	role := strings.TrimSpace(req.Role)
	if role != domain.RoleHR && role != domain.RoleCandidate {
		return nil, status.Error(codes.InvalidArgument, "role must be hr or candidate")
	}
	username := strings.TrimSpace(req.Username)
	if len(username) < 3 || len(req.Password) < 6 {
		return nil, status.Error(codes.InvalidArgument, "username or password is too short")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	user := domain.User{Username: username, PasswordHash: string(hash), Role: role}
	if err := s.db.WithContext(ctx).Create(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, status.Error(codes.AlreadyExists, "username already exists for this role")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &rpc.RegisterResponse{User: userDTO(user)}, nil
}

func (s *Server) Login(ctx context.Context, req *rpc.LoginRequest) (*rpc.LoginResponse, error) {
	var user domain.User
	err := s.db.WithContext(ctx).
		Where("username = ? AND role = ?", strings.TrimSpace(req.Username), strings.TrimSpace(req.Role)).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, status.Error(codes.Unauthenticated, "invalid username or password")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid username or password")
	}
	return &rpc.LoginResponse{User: userDTO(user)}, nil
}

func (s *Server) ListJobs(ctx context.Context, req *rpc.ListJobsRequest) (*rpc.ListJobsResponse, error) {
	page, size := normalizePage(req.Page, req.PageSize)
	q := s.db.WithContext(ctx).Model(&domain.Job{})
	if req.Status != "" {
		q = q.Where("status = ?", req.Status)
	} else if req.Actor == nil {
		q = q.Where("status = ?", domain.JobStatusOpen)
	}
	if req.OnlyMine {
		if err := requireRole(req.Actor, domain.RoleHR); err != nil {
			return nil, err
		}
		q = q.Where("hr_id = ?", req.Actor.UserID)
	}
	if keyword := strings.TrimSpace(req.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("title LIKE ? OR city LIKE ? OR skills LIKE ?", like, like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	var jobs []domain.Job
	if err := q.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&jobs).Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	applicationCounts, err := s.applicationCounts(ctx, jobs)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	items := make([]*rpc.JobDTO, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, jobDTO(job, applicationCounts[job.ID]))
	}
	return &rpc.ListJobsResponse{Items: items, Total: total}, nil
}

func (s *Server) CreateJob(ctx context.Context, req *rpc.CreateJobRequest) (*rpc.JobResponse, error) {
	if err := requireRole(req.Actor, domain.RoleHR); err != nil {
		return nil, err
	}
	if err := validateJob(req.Title, req.City, req.SalaryMin, req.SalaryMax); err != nil {
		return nil, err
	}
	job := domain.Job{
		HRID: req.Actor.UserID, Title: strings.TrimSpace(req.Title), City: strings.TrimSpace(req.City),
		SalaryMin: req.SalaryMin, SalaryMax: req.SalaryMax, Education: strings.TrimSpace(req.Education),
		Experience: strings.TrimSpace(req.Experience), Skills: strings.TrimSpace(req.Skills),
		Description: strings.TrimSpace(req.Description), Status: domain.JobStatusOpen,
	}
	if err := s.db.WithContext(ctx).Create(&job).Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	dto, err := s.jobDTO(ctx, job)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &rpc.JobResponse{Job: dto}, nil
}

func (s *Server) UpdateJob(ctx context.Context, req *rpc.UpdateJobRequest) (*rpc.JobResponse, error) {
	if err := requireRole(req.Actor, domain.RoleHR); err != nil {
		return nil, err
	}
	if err := validateJob(req.Title, req.City, req.SalaryMin, req.SalaryMax); err != nil {
		return nil, err
	}
	var job domain.Job
	if err := s.db.WithContext(ctx).First(&job, req.ID).Error; err != nil {
		return nil, notFoundOrInternal(err, "job not found")
	}
	if job.HRID != req.Actor.UserID {
		return nil, status.Error(codes.PermissionDenied, "cannot update another hr's job")
	}
	job.Title = strings.TrimSpace(req.Title)
	job.City = strings.TrimSpace(req.City)
	job.SalaryMin = req.SalaryMin
	job.SalaryMax = req.SalaryMax
	job.Education = strings.TrimSpace(req.Education)
	job.Experience = strings.TrimSpace(req.Experience)
	job.Skills = strings.TrimSpace(req.Skills)
	job.Description = strings.TrimSpace(req.Description)
	if err := s.db.WithContext(ctx).Save(&job).Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	dto, err := s.jobDTO(ctx, job)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &rpc.JobResponse{Job: dto}, nil
}

func (s *Server) OfflineJob(ctx context.Context, req *rpc.OfflineJobRequest) (*rpc.JobResponse, error) {
	if err := requireRole(req.Actor, domain.RoleHR); err != nil {
		return nil, err
	}
	var job domain.Job
	if err := s.db.WithContext(ctx).First(&job, req.ID).Error; err != nil {
		return nil, notFoundOrInternal(err, "job not found")
	}
	if job.HRID != req.Actor.UserID {
		return nil, status.Error(codes.PermissionDenied, "cannot offline another hr's job")
	}
	job.Status = domain.JobStatusOffline
	if err := s.db.WithContext(ctx).Save(&job).Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	dto, err := s.jobDTO(ctx, job)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &rpc.JobResponse{Job: dto}, nil
}

func (s *Server) GetProfile(ctx context.Context, req *rpc.GetProfileRequest) (*rpc.ProfileResponse, error) {
	if err := requireRole(req.Actor, domain.RoleCandidate); err != nil {
		return nil, err
	}
	var profile domain.CandidateProfile
	err := s.db.WithContext(ctx).First(&profile, "user_id = ?", req.Actor.UserID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &rpc.ProfileResponse{}, nil
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &rpc.ProfileResponse{Profile: profileDTO(profile)}, nil
}

func (s *Server) UpsertProfile(ctx context.Context, req *rpc.UpsertProfileRequest) (*rpc.ProfileResponse, error) {
	if err := requireRole(req.Actor, domain.RoleCandidate); err != nil {
		return nil, err
	}
	workExperience := strings.TrimSpace(req.WorkExperience)
	projectExperience := strings.TrimSpace(req.ProjectExperience)
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Phone) == "" || strings.TrimSpace(req.Education) == "" ||
		strings.TrimSpace(req.School) == "" || strings.TrimSpace(req.Skills) == "" || workExperience == "" || projectExperience == "" {
		return nil, status.Error(codes.InvalidArgument, "profile required fields are incomplete")
	}
	experience := strings.TrimSpace(req.Experience)
	if experience == "" {
		experience = combineProfileExperience(workExperience, projectExperience)
	}
	profile := domain.CandidateProfile{
		UserID: req.Actor.UserID, Name: strings.TrimSpace(req.Name), Phone: strings.TrimSpace(req.Phone),
		Education: strings.TrimSpace(req.Education), School: strings.TrimSpace(req.School),
		Experience: experience, WorkExperience: workExperience, ProjectExperience: projectExperience,
		Skills: strings.TrimSpace(req.Skills),
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "phone", "education", "school", "experience", "work_experience", "project_experience", "skills", "updated_at"}),
	}).Create(&profile).Error
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &rpc.ProfileResponse{Profile: profileDTO(profile)}, nil
}

func (s *Server) SaveResume(ctx context.Context, req *rpc.SaveResumeRequest) (*rpc.ResumeResponse, error) {
	if err := requireRole(req.Actor, domain.RoleCandidate); err != nil {
		return nil, err
	}
	if err := validateResumeMetadata(req.FileName, req.FilePath, req.ContentType, req.Size); err != nil {
		return nil, err
	}
	resume := domain.Resume{
		UserID:      req.Actor.UserID,
		FileName:    filepath.Base(req.FileName),
		ObjectKey:   req.FilePath,
		ContentType: req.ContentType,
		Size:        req.Size,
		Status:      domain.ResumeReady,
	}
	if err := s.db.WithContext(ctx).Create(&resume).Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &rpc.ResumeResponse{Resume: s.resumeDTO(resume)}, nil
}

func (s *Server) GetResume(ctx context.Context, req *rpc.GetResumeRequest) (*rpc.ResumeResponse, error) {
	if req.Actor == nil || req.Actor.UserID == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing actor")
	}
	var resume domain.Resume
	switch req.Actor.Role {
	case domain.RoleCandidate:
		if err := s.db.WithContext(ctx).First(&resume, "id = ? AND user_id = ?", req.ResumeID, req.Actor.UserID).Error; err != nil {
			return nil, notFoundOrInternal(err, "resume not found")
		}
	case domain.RoleHR:
		err := s.db.WithContext(ctx).
			Joins("JOIN applications ON applications.resume_id = resumes.id").
			Joins("JOIN jobs ON jobs.id = applications.job_id AND jobs.hr_id = ?", req.Actor.UserID).
			First(&resume, "resumes.id = ?", req.ResumeID).Error
		if err != nil {
			return nil, notFoundOrInternal(err, "resume not found")
		}
	default:
		return nil, status.Error(codes.PermissionDenied, "invalid role")
	}
	return &rpc.ResumeResponse{Resume: s.resumeDTO(resume)}, nil
}

func (s *Server) ApplyJob(ctx context.Context, req *rpc.ApplyJobRequest) (*rpc.ApplyJobResponse, error) {
	if err := requireRole(req.Actor, domain.RoleCandidate); err != nil {
		return nil, err
	}
	var job domain.Job
	if err := s.db.WithContext(ctx).First(&job, "id = ? AND status = ?", req.JobID, domain.JobStatusOpen).Error; err != nil {
		return nil, notFoundOrInternal(err, "open job not found")
	}
	var profile domain.CandidateProfile
	if err := s.db.WithContext(ctx).First(&profile, "user_id = ?", req.Actor.UserID).Error; err != nil {
		return nil, status.Error(codes.FailedPrecondition, "please complete candidate profile before applying")
	}
	var resume domain.Resume
	if err := s.db.WithContext(ctx).Where("user_id = ? AND status = ?", req.Actor.UserID, domain.ResumeReady).
		Order("created_at DESC").First(&resume).Error; err != nil {
		return nil, status.Error(codes.FailedPrecondition, "please upload a valid resume before applying")
	}
	app := domain.Application{JobID: job.ID, UserID: req.Actor.UserID, ResumeID: resume.ID, Status: domain.ApplicationSubmitted}
	if err := s.db.WithContext(ctx).Create(&app).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, status.Error(codes.AlreadyExists, "job already applied")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	if s.ai != nil {
		if err := s.ai.IndexApplicationResume(ctx, job.HRID, job.ID, app.ID, req.Actor.UserID, resume, profile, job.Title); err != nil {
			log.Printf("index application resume rag failed: hr_id=%d job_id=%d candidate_id=%d resume_id=%d err=%v", job.HRID, job.ID, req.Actor.UserID, resume.ID, err)
		}
	}
	dto, err := s.applicationDTO(ctx, app)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &rpc.ApplyJobResponse{Application: dto}, nil
}

func (s *Server) ListApplications(ctx context.Context, req *rpc.ListApplicationsRequest) (*rpc.ListApplicationsResponse, error) {
	if req.Actor == nil {
		return nil, status.Error(codes.Unauthenticated, "missing actor")
	}
	page, size := normalizePage(req.Page, req.PageSize)
	q := s.db.WithContext(ctx).Model(&domain.Application{})
	if req.Actor.Role == domain.RoleCandidate {
		q = q.Where("user_id = ?", req.Actor.UserID)
	} else if req.Actor.Role == domain.RoleHR {
		q = q.Joins("JOIN jobs ON jobs.id = applications.job_id AND jobs.hr_id = ?", req.Actor.UserID)
		if req.JobID > 0 {
			q = q.Where("applications.job_id = ?", req.JobID)
		}
	} else {
		return nil, status.Error(codes.PermissionDenied, "invalid role")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	var apps []domain.Application
	if err := q.Order("applications.created_at DESC").Offset((page - 1) * size).Limit(size).Find(&apps).Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	items, err := s.applicationDTOs(ctx, apps)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &rpc.ListApplicationsResponse{Items: items, Total: total}, nil
}

func (s *Server) AIChat(ctx context.Context, req *rpc.AIChatRequest) (*rpc.AIChatResponse, error) {
	if err := requireRole(req.Actor, domain.RoleHR); err != nil {
		return nil, err
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		return nil, status.Error(codes.InvalidArgument, "question is required")
	}
	history, err := s.memoryContextMessages(ctx, req.Actor.UserID, question)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	answer, err := s.ai.AnswerWithTools(ctx, req.Actor.UserID, question, history)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	now := time.Now()
	records := chatTurnRecords(req.Actor.UserID, chatTurnID(now), question, answer.Answer, answer.ToolCalls, now)
	if err := s.db.WithContext(ctx).Create(&records).Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &rpc.AIChatResponse{
		Answer:  answer.Answer,
		Context: s.aiResponseContext(answer),
	}, nil
}

func (s *Server) AIChatStream(req *rpc.AIChatRequest, stream rpc.LogicService_AIChatStreamServer) error {
	if err := requireRole(req.Actor, domain.RoleHR); err != nil {
		return err
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		return status.Error(codes.InvalidArgument, "question is required")
	}
	ctx := stream.Context()
	history, err := s.memoryContextMessages(ctx, req.Actor.UserID, question)
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	var full strings.Builder
	answer, err := s.ai.StreamWithTools(ctx, req.Actor.UserID, question, history, func(chunk string) error {
		full.WriteString(chunk)
		return stream.Send(&rpc.AIChatStreamChunk{Content: chunk})
	})
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	finalAnswer := strings.TrimSpace(full.String())
	if finalAnswer == "" {
		return status.Error(codes.Internal, "agent returned empty answer")
	}
	now := time.Now()
	records := chatTurnRecords(req.Actor.UserID, chatTurnID(now), question, finalAnswer, answer.ToolCalls, now)
	if err := s.db.WithContext(ctx).Create(&records).Error; err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	return stream.Send(&rpc.AIChatStreamChunk{
		Done:    true,
		Context: s.aiResponseContext(answer),
	})
}

func (s *Server) aiResponseContext(answer ai.AgentAnswer) map[string]string {
	context := map[string]string{
		"agent":      "ChatModelAgent",
		"used_tools": strings.Join(answer.UsedTools, ","),
	}
	if s.showToolResults {
		context["tool_calls"] = toolCallsContext(answer.ToolCalls)
	}
	return context
}

func (s *Server) ListChatHistory(ctx context.Context, req *rpc.ListChatHistoryRequest) (*rpc.ListChatHistoryResponse, error) {
	if err := requireRole(req.Actor, domain.RoleHR); err != nil {
		return nil, err
	}
	limit := int(req.Limit)
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	q := s.db.WithContext(ctx).Where("hr_id = ? AND role <> ?", req.Actor.UserID, domain.ChatRoleTool)
	var rows []domain.ChatMessage
	if err := q.Order("created_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })
	items := make([]*rpc.ChatMessageDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, &rpc.ChatMessageDTO{
			ID: row.ID, HRID: row.HRID, TurnID: row.TurnID, Role: row.Role, ToolName: row.ToolName, Content: row.Content, CreatedAt: rpc.FormatTime(row.CreatedAt),
		})
	}
	return &rpc.ListChatHistoryResponse{Items: items}, nil
}

func (s *Server) memoryContextMessages(ctx context.Context, hrID uint64, currentQuestion string) ([]*schema.Message, error) {
	rounds := s.memoryRounds
	if rounds <= 0 {
		rounds = 5
	}
	summary, err := s.loadMemorySummary(ctx, hrID)
	if err != nil {
		return nil, err
	}
	recentTurnIDs, err := s.recentTurnIDs(ctx, hrID, rounds)
	if err != nil {
		return nil, err
	}
	pendingRows, err := s.pendingUnsummarizedRows(ctx, hrID, summary.LastSummarizedAt, recentTurnIDs)
	if err != nil {
		return nil, err
	}
	recentRows, err := s.chatRowsForTurns(ctx, hrID, recentTurnIDs, rounds)
	if err != nil {
		return nil, err
	}

	messages := s.buildMemoryMessages(summary.Summary, pendingRows, recentRows)
	if len(pendingRows) > 0 && estimateMessagesTokens(messages, currentQuestion) >= s.memoryTriggerTokens {
		newSummary, summarizeErr := s.summarizePendingRows(ctx, hrID, summary, pendingRows)
		if summarizeErr != nil {
			return nil, fmt.Errorf("summarize chat memory: %w", summarizeErr)
		}
		messages = s.buildMemoryMessages(newSummary.Summary, nil, recentRows)
	}
	return messages, nil
}

func (s *Server) loadMemorySummary(ctx context.Context, hrID uint64) (domain.ChatMemorySummary, error) {
	var summary domain.ChatMemorySummary
	err := s.db.WithContext(ctx).First(&summary, "hr_id = ?", hrID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ChatMemorySummary{HRID: hrID}, nil
	}
	return summary, err
}

func (s *Server) recentTurnIDs(ctx context.Context, hrID uint64, rounds int) ([]string, error) {
	type turnRow struct {
		TurnID       string
		MaxCreatedAt time.Time
	}
	var turns []turnRow
	if err := s.db.WithContext(ctx).Model(&domain.ChatMessage{}).
		Select("turn_id, MAX(created_at) AS max_created_at").
		Where("hr_id = ? AND turn_id <> ?", hrID, "").
		Group("turn_id").
		Order("max_created_at DESC").
		Limit(rounds).
		Scan(&turns).Error; err != nil {
		return nil, err
	}
	turnIDs := make([]string, 0, len(turns))
	for _, turn := range turns {
		turnIDs = append(turnIDs, turn.TurnID)
	}
	return turnIDs, nil
}

func (s *Server) pendingUnsummarizedRows(ctx context.Context, hrID uint64, after time.Time, recentTurnIDs []string) ([]domain.ChatMessage, error) {
	q := s.db.WithContext(ctx).
		Where("hr_id = ? AND turn_id <> ? AND created_at > ?", hrID, "", after)
	if len(recentTurnIDs) > 0 {
		q = q.Where("turn_id NOT IN ?", recentTurnIDs)
	}
	var rows []domain.ChatMessage
	if err := q.Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Server) chatRowsForTurns(ctx context.Context, hrID uint64, turnIDs []string, fallbackRounds int) ([]domain.ChatMessage, error) {
	var rows []domain.ChatMessage
	if len(turnIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("hr_id = ? AND turn_id IN ?", hrID, turnIDs).
			Order("created_at ASC").Find(&rows).Error; err != nil {
			return nil, err
		}
	} else {
		if err := s.db.WithContext(ctx).Where("hr_id = ?", hrID).Order("created_at DESC").Limit(fallbackRounds * 2).Find(&rows).Error; err != nil {
			return nil, err
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.Before(rows[j].CreatedAt) })
	}
	return rows, nil
}

func (s *Server) buildMemoryMessages(summary string, pendingRows []domain.ChatMessage, recentRows []domain.ChatMessage) []*schema.Message {
	messages := make([]*schema.Message, 0, len(pendingRows)+len(recentRows)+1)
	if strings.TrimSpace(summary) != "" {
		messages = append(messages, schema.SystemMessage(memorySummaryContext(summary)))
	}
	messages = append(messages, s.chatRowsToMessages(pendingRows)...)
	messages = append(messages, s.chatRowsToMessages(recentRows)...)
	return messages
}

func (s *Server) chatRowsToMessages(rows []domain.ChatMessage) []*schema.Message {
	messages := make([]*schema.Message, 0, len(rows))
	for _, row := range rows {
		switch row.Role {
		case domain.ChatRoleUser:
			messages = append(messages, schema.UserMessage(row.Content))
		case domain.ChatRoleTool:
			messages = append(messages, schema.SystemMessage(toolMemoryContent(row, s.showToolResults)))
		case domain.ChatRoleAssistant:
			messages = append(messages, schema.AssistantMessage(row.Content, nil))
		}
	}
	return messages
}

func (s *Server) summarizePendingRows(ctx context.Context, hrID uint64, summary domain.ChatMemorySummary, rows []domain.ChatMessage) (domain.ChatMemorySummary, error) {
	if s.ai == nil {
		return summary, fmt.Errorf("ai client is nil")
	}
	if len(rows) == 0 {
		return summary, nil
	}
	pendingText := rowsForSummary(rows)
	if pendingText == "" {
		return summary, nil
	}
	newText, err := s.ai.SummarizeMemory(ctx, summary.Summary, pendingText)
	if err != nil {
		return summary, err
	}
	summary.HRID = hrID
	summary.Summary = newText
	summary.LastSummarizedAt = rows[len(rows)-1].CreatedAt
	summary.SummaryVersion = 1
	if err := s.db.WithContext(ctx).Save(&summary).Error; err != nil {
		return summary, err
	}
	return summary, nil
}

func memorySummaryContext(summary string) string {
	return fmt.Sprintf("以下是前文滚动摘要，仅用于理解用户长期意图、偏好和已确认设计；其中涉及实时招聘数据的内容不可直接作为事实使用，必须重新调用工具查询。\n\n%s", strings.TrimSpace(summary))
}

func rowsForSummary(rows []domain.ChatMessage) string {
	var b strings.Builder
	for _, row := range rows {
		switch row.Role {
		case domain.ChatRoleUser:
			b.WriteString("用户：")
			b.WriteString(strings.TrimSpace(row.Content))
			b.WriteString("\n")
		case domain.ChatRoleAssistant:
			b.WriteString("助手：")
			b.WriteString(strings.TrimSpace(row.Content))
			b.WriteString("\n")
		case domain.ChatRoleTool:
			toolName := row.ToolName
			if toolName == "" {
				toolName = toolNameFromRecord(row.Content)
			}
			if toolName == "" {
				toolName = "unknown"
			}
			b.WriteString("工具调用：")
			b.WriteString(toolName)
			b.WriteString("（工具参数和结果不写入长期摘要；涉及实时数据需重新查询）\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func toolNameFromRecord(content string) string {
	var record ai.ToolCallRecord
	if err := json.Unmarshal([]byte(content), &record); err != nil {
		return ""
	}
	return record.Name
}

func estimateMessagesTokens(messages []*schema.Message, currentQuestion string) int {
	total := estimateTextTokens(currentQuestion)
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		total += estimateTextTokens(msg.Content)
		total += 4
	}
	return total
}

func estimateTextTokens(value string) int {
	tokens := 0
	asciiRunes := 0
	for _, r := range value {
		if r <= 127 {
			asciiRunes++
			continue
		}
		if asciiRunes > 0 {
			tokens += (asciiRunes + 3) / 4
			asciiRunes = 0
		}
		tokens++
	}
	if asciiRunes > 0 {
		tokens += (asciiRunes + 3) / 4
	}
	return tokens
}

func chatTurnRecords(hrID uint64, turnID string, question string, answer string, tools []ai.ToolCallRecord, now time.Time) []domain.ChatMessage {
	records := make([]domain.ChatMessage, 0, len(tools)+2)
	records = append(records, domain.ChatMessage{
		HRID:      hrID,
		TurnID:    turnID,
		Role:      domain.ChatRoleUser,
		Content:   question,
		CreatedAt: now,
	})
	for i, toolCall := range tools {
		records = append(records, domain.ChatMessage{
			HRID:      hrID,
			TurnID:    turnID,
			Role:      domain.ChatRoleTool,
			ToolName:  toolCall.Name,
			Content:   marshalToolRecord(toolCall),
			CreatedAt: now.Add(time.Duration(i+1) * time.Millisecond),
		})
	}
	records = append(records, domain.ChatMessage{
		HRID:      hrID,
		TurnID:    turnID,
		Role:      domain.ChatRoleAssistant,
		Content:   answer,
		CreatedAt: now.Add(time.Duration(len(tools)+1) * time.Millisecond),
	})
	return records
}

func chatTurnID(now time.Time) string {
	return fmt.Sprintf("%d", now.UnixNano())
}

func marshalToolRecord(record ai.ToolCallRecord) string {
	record.Arguments = truncateForChatMemory(record.Arguments)
	record.Result = truncateForChatMemory(record.Result)
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Sprintf(`{"name":%q,"error":%q}`, record.Name, err.Error())
	}
	return string(data)
}

func toolCallsContext(records []ai.ToolCallRecord) string {
	if len(records) == 0 {
		return "[]"
	}
	data, err := json.Marshal(records)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func toolMemoryContent(row domain.ChatMessage, includeResult bool) string {
	toolName := row.ToolName
	if toolName == "" {
		toolName = "unknown"
	}
	var record ai.ToolCallRecord
	if err := json.Unmarshal([]byte(row.Content), &record); err != nil {
		if includeResult {
			return fmt.Sprintf("历史工具调用记录：tool=%s result=%s", toolName, truncateForChatMemory(row.Content))
		}
		return fmt.Sprintf("历史工具调用记录：tool=%s result=[历史检索结果已隐藏，请根据当前问题重新检索]", toolName)
	}
	if record.Name != "" {
		toolName = record.Name
	}
	result := "[历史检索结果已隐藏，请根据当前问题重新检索]"
	if includeResult {
		result = truncateForChatMemory(record.Result)
		if record.Error != "" {
			result = "error: " + record.Error
		}
	}
	return fmt.Sprintf("历史工具调用记录：tool=%s arguments=%s result=%s", toolName, truncateForChatMemory(record.Arguments), result)
}

func truncateForChatMemory(value string) string {
	const max = 12000
	value = strings.TrimSpace(value)
	if len([]rune(value)) <= max {
		return value
	}
	return string([]rune(value)[:max]) + "...[truncated]"
}

func requireRole(actor *rpc.Actor, role string) error {
	if actor == nil || actor.UserID == 0 {
		return status.Error(codes.Unauthenticated, "missing actor")
	}
	if actor.Role != role {
		return status.Error(codes.PermissionDenied, "role not allowed")
	}
	return nil
}

func validateJob(title, city string, minSalary, maxSalary int32) error {
	if strings.TrimSpace(title) == "" || strings.TrimSpace(city) == "" {
		return status.Error(codes.InvalidArgument, "title and city are required")
	}
	if minSalary < 0 || maxSalary < 0 || (maxSalary > 0 && minSalary > maxSalary) {
		return status.Error(codes.InvalidArgument, "invalid salary range")
	}
	return nil
}

func validateResumeMetadata(fileName, filePath, contentType string, size int64) error {
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext != ".pdf" && ext != ".doc" && ext != ".docx" {
		return status.Error(codes.InvalidArgument, "resume only supports pdf, doc, docx")
	}
	if strings.TrimSpace(filePath) == "" {
		return status.Error(codes.InvalidArgument, "resume file path is required")
	}
	if strings.TrimSpace(contentType) == "" {
		return status.Error(codes.InvalidArgument, "resume content type is required")
	}
	if size <= 0 || size > 20*1024*1024 {
		return status.Error(codes.InvalidArgument, "resume size must be within 20MB")
	}
	return nil
}

func normalizePage(page, size int32) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	return int(page), int(size)
}

func notFoundOrInternal(err error, msg string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return status.Error(codes.NotFound, msg)
	}
	return status.Error(codes.Internal, err.Error())
}

func userDTO(user domain.User) *rpc.UserDTO {
	return &rpc.UserDTO{ID: user.ID, Username: user.Username, Role: user.Role, CreatedAt: rpc.FormatTime(user.CreatedAt)}
}

func profileDTO(profile domain.CandidateProfile) *rpc.CandidateProfileDTO {
	workExperience := profile.WorkExperience
	projectExperience := profile.ProjectExperience
	if workExperience == "" && projectExperience == "" && profile.Experience != "" {
		workExperience = profile.Experience
	}
	return &rpc.CandidateProfileDTO{
		UserID: profile.UserID, Name: profile.Name, Phone: profile.Phone, Education: profile.Education,
		School: profile.School, Experience: profile.Experience, WorkExperience: workExperience,
		ProjectExperience: projectExperience, Skills: profile.Skills,
		UpdatedAt: rpc.FormatTime(profile.UpdatedAt),
	}
}

func combineProfileExperience(workExperience, projectExperience string) string {
	var b strings.Builder
	if strings.TrimSpace(workExperience) != "" {
		b.WriteString("## 工作经历\n\n")
		b.WriteString(strings.TrimSpace(workExperience))
	}
	if strings.TrimSpace(projectExperience) != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## 项目经历\n\n")
		b.WriteString(strings.TrimSpace(projectExperience))
	}
	return b.String()
}

func (s *Server) applicationCounts(ctx context.Context, jobs []domain.Job) (map[uint64]int64, error) {
	if len(jobs) == 0 {
		return map[uint64]int64{}, nil
	}
	ids := make([]uint64, 0, len(jobs))
	for _, job := range jobs {
		ids = append(ids, job.ID)
	}
	return s.applicationCountsByJobIDs(ctx, ids)
}

func (s *Server) applicationCountsByJobIDs(ctx context.Context, ids []uint64) (map[uint64]int64, error) {
	if len(ids) == 0 {
		return map[uint64]int64{}, nil
	}
	var rows []struct {
		JobID uint64
		Count int64
	}
	if err := s.db.WithContext(ctx).Model(&domain.Application{}).
		Select("job_id, COUNT(*) AS count").
		Where("job_id IN ?", ids).
		Group("job_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[uint64]int64, len(rows))
	for _, row := range rows {
		counts[row.JobID] = row.Count
	}
	return counts, nil
}

func (s *Server) jobDTO(ctx context.Context, job domain.Job) (*rpc.JobDTO, error) {
	var applicationNum int64
	if err := s.db.WithContext(ctx).Model(&domain.Application{}).Where("job_id = ?", job.ID).Count(&applicationNum).Error; err != nil {
		return nil, err
	}
	return jobDTO(job, applicationNum), nil
}

func jobDTO(job domain.Job, applicationNum int64) *rpc.JobDTO {
	return &rpc.JobDTO{
		ID: job.ID, HRID: job.HRID, Title: job.Title, City: job.City, SalaryMin: job.SalaryMin, SalaryMax: job.SalaryMax,
		Education: job.Education, Experience: job.Experience, Skills: job.Skills, Description: job.Description,
		Status: job.Status, ApplicationNum: applicationNum, CreatedAt: rpc.FormatTime(job.CreatedAt), UpdatedAt: rpc.FormatTime(job.UpdatedAt),
	}
}

func (s *Server) resumeDTO(resume domain.Resume) *rpc.ResumeDTO {
	return &rpc.ResumeDTO{
		ID: resume.ID, UserID: resume.UserID, FileName: resume.FileName, ObjectKey: resume.ObjectKey,
		FilePath: resume.ObjectKey, ContentType: resume.ContentType, Size: resume.Size, Status: resume.Status,
		DownloadURL: fmt.Sprintf("/api/v1/hr/resumes/%d/download", resume.ID),
		CreatedAt:   rpc.FormatTime(resume.CreatedAt),
	}
}

func (s *Server) applicationDTO(ctx context.Context, app domain.Application) (*rpc.ApplicationDTO, error) {
	items, err := s.applicationDTOs(ctx, []domain.Application{app})
	if err != nil {
		return nil, err
	}
	return items[0], nil
}

func (s *Server) applicationDTOs(ctx context.Context, apps []domain.Application) ([]*rpc.ApplicationDTO, error) {
	if len(apps) == 0 {
		return []*rpc.ApplicationDTO{}, nil
	}
	jobIDs := make([]uint64, 0, len(apps))
	userIDs := make([]uint64, 0, len(apps))
	resumeIDs := make([]uint64, 0, len(apps))
	seenJobs := make(map[uint64]struct{}, len(apps))
	seenUsers := make(map[uint64]struct{}, len(apps))
	seenResumes := make(map[uint64]struct{}, len(apps))
	for _, app := range apps {
		if _, ok := seenJobs[app.JobID]; !ok {
			seenJobs[app.JobID] = struct{}{}
			jobIDs = append(jobIDs, app.JobID)
		}
		if _, ok := seenUsers[app.UserID]; !ok {
			seenUsers[app.UserID] = struct{}{}
			userIDs = append(userIDs, app.UserID)
		}
		if _, ok := seenResumes[app.ResumeID]; !ok {
			seenResumes[app.ResumeID] = struct{}{}
			resumeIDs = append(resumeIDs, app.ResumeID)
		}
	}

	var jobs []domain.Job
	if err := s.db.WithContext(ctx).Find(&jobs, jobIDs).Error; err != nil {
		return nil, err
	}
	var users []domain.User
	if err := s.db.WithContext(ctx).Find(&users, userIDs).Error; err != nil {
		return nil, err
	}
	var profiles []domain.CandidateProfile
	if err := s.db.WithContext(ctx).Where("user_id IN ?", userIDs).Find(&profiles).Error; err != nil {
		return nil, err
	}
	var resumes []domain.Resume
	if err := s.db.WithContext(ctx).Find(&resumes, resumeIDs).Error; err != nil {
		return nil, err
	}
	applicationCounts, err := s.applicationCountsByJobIDs(ctx, jobIDs)
	if err != nil {
		return nil, err
	}

	jobsByID := make(map[uint64]domain.Job, len(jobs))
	for _, job := range jobs {
		jobsByID[job.ID] = job
	}
	usersByID := make(map[uint64]domain.User, len(users))
	for _, user := range users {
		usersByID[user.ID] = user
	}
	profilesByUserID := make(map[uint64]domain.CandidateProfile, len(profiles))
	for _, profile := range profiles {
		profilesByUserID[profile.UserID] = profile
	}
	resumesByID := make(map[uint64]domain.Resume, len(resumes))
	for _, resume := range resumes {
		resumesByID[resume.ID] = resume
	}

	items := make([]*rpc.ApplicationDTO, 0, len(apps))
	for _, app := range apps {
		job, ok := jobsByID[app.JobID]
		if !ok {
			return nil, fmt.Errorf("application %d references missing job %d", app.ID, app.JobID)
		}
		user, ok := usersByID[app.UserID]
		if !ok {
			return nil, fmt.Errorf("application %d references missing user %d", app.ID, app.UserID)
		}
		profile, ok := profilesByUserID[app.UserID]
		if !ok {
			return nil, fmt.Errorf("application %d references missing profile for user %d", app.ID, app.UserID)
		}
		resume, ok := resumesByID[app.ResumeID]
		if !ok {
			return nil, fmt.Errorf("application %d references missing resume %d", app.ID, app.ResumeID)
		}
		items = append(items, &rpc.ApplicationDTO{
			ID: app.ID, Job: jobDTO(job, applicationCounts[job.ID]), Candidate: userDTO(user), Profile: profileDTO(profile),
			Resume: s.resumeDTO(resume), Status: app.Status, CreatedAt: rpc.FormatTime(app.CreatedAt),
		})
	}
	return items, nil
}
