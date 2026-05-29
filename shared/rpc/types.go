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

type ChatMessageDTO struct {
	ID        uint64 `json:"id"`
	HRID      uint64 `json:"hr_id"`
	TurnID    string `json:"turn_id,omitempty"`
	Role      string `json:"role"`
	ToolName  string `json:"tool_name,omitempty"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

type AIChatRequest struct {
	Actor    *Actor `json:"actor"`
	Question string `json:"question"`
}

type AIChatResponse struct {
	Answer  string            `json:"answer"`
	Context map[string]string `json:"context"`
}

type AIChatStreamChunk struct {
	Content string            `json:"content"`
	Done    bool              `json:"done"`
	Context map[string]string `json:"context,omitempty"`
}

type ListChatHistoryRequest struct {
	Actor *Actor `json:"actor"`
	Limit int32  `json:"limit"`
}

type ListChatHistoryResponse struct {
	Items []*ChatMessageDTO `json:"items"`
}

func FormatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
