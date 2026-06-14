package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

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
	db              *gorm.DB
	ai              *ai.Client
	recommendations *RecommendationQueue
}

type Option func(*Server)

func NewServer(db *gorm.DB, aiClient *ai.Client, opts ...Option) *Server {
	s := &Server{db: db, ai: aiClient}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func WithRecommendationQueue(queue *RecommendationQueue) Option {
	return func(s *Server) {
		s.recommendations = queue
	}
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

func (s *Server) RecommendResumes(ctx context.Context, req *rpc.ResumeRecommendationRequest) (*rpc.ResumeRecommendationResponse, error) {
	input, err := s.validateResumeRecommendationRequest(req)
	if err != nil {
		return nil, err
	}
	out, err := s.ai.RecommendResumesByJD(ctx, req.Actor.UserID, input)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resumeRecommendationResponse(out), nil
}

func (s *Server) CreateResumeRecommendationTask(ctx context.Context, req *rpc.ResumeRecommendationRequest) (*rpc.ResumeRecommendationTaskResponse, error) {
	input, err := s.validateResumeRecommendationRequest(req)
	if err != nil {
		return nil, err
	}
	if s.recommendations == nil {
		return nil, status.Error(codes.FailedPrecondition, "resume recommendation queue is unavailable")
	}
	return s.recommendations.Enqueue(ctx, req.Actor.UserID, input)
}

func (s *Server) RecommendResumesStream(req *rpc.ResumeRecommendationRequest, stream rpc.ResumeRecommendationService_RecommendResumesStreamServer) error {
	input, err := s.validateResumeRecommendationRequest(req)
	if err != nil {
		return err
	}
	if err := stream.Send(&rpc.ResumeRecommendationStreamChunk{
		Stage:   "accepted",
		Message: "已接收简历推荐请求",
	}); err != nil {
		return err
	}
	out, err := s.ai.RecommendResumesByJDWithProgress(stream.Context(), req.Actor.UserID, input, func(stage string, message string) error {
		return stream.Send(&rpc.ResumeRecommendationStreamChunk{
			Stage:   stage,
			Message: message,
		})
	})
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	return stream.Send(&rpc.ResumeRecommendationStreamChunk{
		Stage:    "done",
		Message:  "简历推荐完成",
		Done:     true,
		Response: resumeRecommendationResponse(out),
	})
}

func (s *Server) WatchResumeRecommendationTask(req *rpc.ResumeRecommendationTaskWatchRequest, stream rpc.ResumeRecommendationService_WatchResumeRecommendationTaskServer) error {
	if err := requireRole(req.Actor, domain.RoleHR); err != nil {
		return err
	}
	if s.recommendations == nil {
		return status.Error(codes.FailedPrecondition, "resume recommendation queue is unavailable")
	}
	return s.recommendations.Watch(stream.Context(), req.Actor, req.TaskID, stream.Send)
}

func (s *Server) validateResumeRecommendationRequest(req *rpc.ResumeRecommendationRequest) (ai.ResumeRecommendationInput, error) {
	if err := requireRole(req.Actor, domain.RoleHR); err != nil {
		return ai.ResumeRecommendationInput{}, err
	}
	if s.ai == nil {
		return ai.ResumeRecommendationInput{}, status.Error(codes.FailedPrecondition, "resume recommendation is unavailable")
	}
	if req.JobID == 0 && strings.TrimSpace(req.JobDescription) == "" && len(req.Queries) == 0 {
		return ai.ResumeRecommendationInput{}, status.Error(codes.InvalidArgument, "job_id, job_description, or queries is required")
	}
	return ai.ResumeRecommendationInput{
		JobID:          req.JobID,
		JobDescription: req.JobDescription,
		Queries:        req.Queries,
		Limit:          int(req.Limit),
		EvidenceLimit:  int(req.EvidenceLimit),
	}, nil
}

func resumeRecommendationResponse(out ai.ResumeRecommendationOutput) *rpc.ResumeRecommendationResponse {
	candidates := make([]*rpc.ResumeRecommendationCandidateDTO, 0, len(out.Candidates))
	for _, candidate := range out.Candidates {
		candidates = append(candidates, resumeRecommendationCandidateDTO(candidate))
	}
	return &rpc.ResumeRecommendationResponse{
		Scope:          out.Scope,
		JobID:          out.JobID,
		JobTitle:       out.JobTitle,
		AgentStatus:    out.AgentStatus,
		FallbackReason: out.FallbackReason,
		Candidates:     candidates,
	}
}

func resumeRecommendationCandidateDTO(candidate ai.ResumeRecommendationCandidate) *rpc.ResumeRecommendationCandidateDTO {
	evidence := make([]*rpc.ResumeRecommendationEvidence, 0, len(candidate.Evidence))
	for _, item := range candidate.Evidence {
		evidence = append(evidence, resumeRecommendationEvidenceDTO(item))
	}
	return &rpc.ResumeRecommendationCandidateDTO{
		CandidateID:   candidate.CandidateID,
		CandidateName: candidate.CandidateName,
		ResumeID:      candidate.ResumeID,
		ResumeName:    candidate.ResumeName,
		JobID:         candidate.JobID,
		JobTitle:      candidate.JobTitle,
		Score:         candidate.Score,
		SemanticScore: candidate.SemanticScore,
		KeywordScore:  candidate.KeywordScore,
		Reason:        candidate.Reason,
		RiskPoints:    candidate.RiskPoints,
		Evidence:      evidence,
	}
}

func resumeRecommendationEvidenceDTO(item ai.ResumeSemanticSearchItem) *rpc.ResumeRecommendationEvidence {
	return &rpc.ResumeRecommendationEvidence{
		Score:           item.Score,
		SemanticScore:   item.SemanticScore,
		KeywordScore:    item.KeywordScore,
		ChunkID:         item.ChunkID,
		ResumeID:        item.ResumeID,
		JobID:           item.JobID,
		JobTitle:        item.JobTitle,
		CandidateID:     item.CandidateID,
		CandidateName:   item.CandidateName,
		SectionType:     item.SectionType,
		SectionTitle:    item.SectionTitle,
		ExperienceIndex: item.ExperienceIndex,
		ChunkIndex:      item.ChunkIndex,
		Content:         item.Content,
		ResumeName:      item.ResumeName,
	}
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
