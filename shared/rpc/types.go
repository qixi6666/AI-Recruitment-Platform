package rpc

import "time"

const (
	RoleHR        = "hr"
	RoleCandidate = "candidate"
)

type Empty struct{}

type PageRequest struct {
	Page     int32 `json:"page"`
	PageSize int32 `json:"page_size"`
}

type UserDTO struct {
	ID        uint64 `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type RegisterResponse struct {
	User *UserDTO `json:"user"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type LoginResponse struct {
	User *UserDTO `json:"user"`
}

type Actor struct {
	UserID uint64 `json:"user_id"`
	Role   string `json:"role"`
}

type JobDTO struct {
	ID             uint64 `json:"id"`
	HRID           uint64 `json:"hr_id"`
	Title          string `json:"title"`
	City           string `json:"city"`
	SalaryMin      int32  `json:"salary_min"`
	SalaryMax      int32  `json:"salary_max"`
	Education      string `json:"education"`
	Experience     string `json:"experience"`
	Skills         string `json:"skills"`
	Description    string `json:"description"`
	Status         string `json:"status"`
	ApplicationNum int64  `json:"application_num"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type ListJobsRequest struct {
	Page     int32  `json:"page"`
	PageSize int32  `json:"page_size"`
	Status   string `json:"status"`
	Keyword  string `json:"keyword"`
	Actor    *Actor `json:"actor,omitempty"`
	OnlyMine bool   `json:"only_mine"`
}

type ListJobsResponse struct {
	Items []*JobDTO `json:"items"`
	Total int64     `json:"total"`
}

type CreateJobRequest struct {
	Actor       *Actor `json:"actor"`
	Title       string `json:"title"`
	City        string `json:"city"`
	SalaryMin   int32  `json:"salary_min"`
	SalaryMax   int32  `json:"salary_max"`
	Education   string `json:"education"`
	Experience  string `json:"experience"`
	Skills      string `json:"skills"`
	Description string `json:"description"`
}

type JobResponse struct {
	Job *JobDTO `json:"job"`
}

type UpdateJobRequest struct {
	Actor       *Actor `json:"actor"`
	ID          uint64 `json:"id"`
	Title       string `json:"title"`
	City        string `json:"city"`
	SalaryMin   int32  `json:"salary_min"`
	SalaryMax   int32  `json:"salary_max"`
	Education   string `json:"education"`
	Experience  string `json:"experience"`
	Skills      string `json:"skills"`
	Description string `json:"description"`
}

type OfflineJobRequest struct {
	Actor *Actor `json:"actor"`
	ID    uint64 `json:"id"`
}

type CandidateProfileDTO struct {
	UserID            uint64 `json:"user_id"`
	Name              string `json:"name"`
	Phone             string `json:"phone"`
	Education         string `json:"education"`
	School            string `json:"school"`
	Experience        string `json:"experience"`
	WorkExperience    string `json:"work_experience"`
	ProjectExperience string `json:"project_experience"`
	Skills            string `json:"skills"`
	UpdatedAt         string `json:"updated_at"`
}

type GetProfileRequest struct {
	Actor *Actor `json:"actor"`
}

type ProfileResponse struct {
	Profile *CandidateProfileDTO `json:"profile"`
}

type UpsertProfileRequest struct {
	Actor             *Actor `json:"actor"`
	Name              string `json:"name"`
	Phone             string `json:"phone"`
	Education         string `json:"education"`
	School            string `json:"school"`
	Experience        string `json:"experience"`
	WorkExperience    string `json:"work_experience"`
	ProjectExperience string `json:"project_experience"`
	Skills            string `json:"skills"`
}

type ResumeDTO struct {
	ID          uint64 `json:"id"`
	UserID      uint64 `json:"user_id"`
	FileName    string `json:"file_name"`
	ObjectKey   string `json:"object_key"`
	FilePath    string `json:"file_path"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Status      string `json:"status"`
	DownloadURL string `json:"download_url,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type SaveResumeRequest struct {
	Actor       *Actor `json:"actor"`
	FileName    string `json:"file_name"`
	FilePath    string `json:"file_path"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type GetResumeRequest struct {
	Actor    *Actor `json:"actor"`
	ResumeID uint64 `json:"resume_id"`
}

type ResumeResponse struct {
	Resume *ResumeDTO `json:"resume"`
}

type ApplyJobRequest struct {
	Actor *Actor `json:"actor"`
	JobID uint64 `json:"job_id"`
}

type ApplicationDTO struct {
	ID        uint64               `json:"id"`
	Job       *JobDTO              `json:"job,omitempty"`
	Candidate *UserDTO             `json:"candidate,omitempty"`
	Profile   *CandidateProfileDTO `json:"profile,omitempty"`
	Resume    *ResumeDTO           `json:"resume,omitempty"`
	Status    string               `json:"status"`
	CreatedAt string               `json:"created_at"`
}

type ApplyJobResponse struct {
	Application *ApplicationDTO `json:"application"`
}

type ListApplicationsRequest struct {
	Actor    *Actor `json:"actor"`
	Page     int32  `json:"page"`
	PageSize int32  `json:"page_size"`
	JobID    uint64 `json:"job_id"`
}

type ListApplicationsResponse struct {
	Items []*ApplicationDTO `json:"items"`
	Total int64             `json:"total"`
}

type ResumeRecommendationRequest struct {
	Actor          *Actor   `json:"actor"`
	JobID          uint64   `json:"job_id"`
	JobDescription string   `json:"job_description"`
	Queries        []string `json:"queries"`
	Limit          int32    `json:"limit"`
	EvidenceLimit  int32    `json:"evidence_limit"`
}

type ResumeRecommendationResponse struct {
	Scope          string                              `json:"scope"`
	JobID          uint64                              `json:"job_id,omitempty"`
	JobTitle       string                              `json:"job_title,omitempty"`
	AgentStatus    string                              `json:"agent_status"`
	FallbackReason string                              `json:"fallback_reason,omitempty"`
	Candidates     []*ResumeRecommendationCandidateDTO `json:"candidates"`
}

type ResumeRecommendationTaskResponse struct {
	TaskID    string                        `json:"task_id"`
	Status    string                        `json:"status"`
	CreatedAt string                        `json:"created_at,omitempty"`
	StreamURL string                        `json:"stream_url,omitempty"`
	Cached    bool                          `json:"cached,omitempty"`
	Response  *ResumeRecommendationResponse `json:"response,omitempty"`
}

type ResumeRecommendationTaskWatchRequest struct {
	Actor  *Actor `json:"actor"`
	TaskID string `json:"task_id"`
}

type ResumeRecommendationStreamChunk struct {
	TaskID   string                        `json:"task_id,omitempty"`
	Status   string                        `json:"status,omitempty"`
	Stage    string                        `json:"stage"`
	Message  string                        `json:"message"`
	Done     bool                          `json:"done"`
	Response *ResumeRecommendationResponse `json:"response,omitempty"`
}

type ResumeRecommendationCandidateDTO struct {
	CandidateID   uint64                          `json:"candidate_id"`
	CandidateName string                          `json:"candidate_name"`
	ResumeID      uint64                          `json:"resume_id"`
	ResumeName    string                          `json:"resume_name"`
	JobID         uint64                          `json:"job_id"`
	JobTitle      string                          `json:"job_title"`
	Score         float32                         `json:"score"`
	SemanticScore float32                         `json:"semantic_score,omitempty"`
	KeywordScore  float32                         `json:"keyword_score,omitempty"`
	Reason        string                          `json:"reason,omitempty"`
	RiskPoints    []string                        `json:"risk_points,omitempty"`
	Evidence      []*ResumeRecommendationEvidence `json:"evidence"`
}

type ResumeRecommendationEvidence struct {
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

func FormatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
