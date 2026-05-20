package domain

import "time"

const (
	RoleHR        = "hr"
	RoleCandidate = "candidate"

	JobStatusOpen    = "open"
	JobStatusOffline = "offline"

	ResumePending = "pending"
	ResumeReady   = "ready"

	ApplicationSubmitted = "submitted"

	ChatRoleUser      = "user"
	ChatRoleAssistant = "assistant"
)

type User struct {
	ID           uint64 `gorm:"primaryKey"`
	Username     string `gorm:"size:64;not null;uniqueIndex:idx_users_username_role"`
	PasswordHash string `gorm:"size:128;not null"`
	Role         string `gorm:"size:32;not null;uniqueIndex:idx_users_username_role"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Job struct {
	ID          uint64 `gorm:"primaryKey"`
	HRID        uint64 `gorm:"not null;index;index:idx_jobs_hr_status_created,priority:1"`
	Title       string `gorm:"size:128;not null;index"`
	City        string `gorm:"size:64;not null;index"`
	SalaryMin   int32
	SalaryMax   int32
	Education   string    `gorm:"size:64"`
	Experience  string    `gorm:"size:64"`
	Skills      string    `gorm:"size:512"`
	Description string    `gorm:"type:text"`
	Status      string    `gorm:"size:32;not null;index;index:idx_jobs_status_created,priority:1;index:idx_jobs_hr_status_created,priority:2"`
	CreatedAt   time.Time `gorm:"index:idx_jobs_status_created,priority:2;index:idx_jobs_hr_status_created,priority:3"`
	UpdatedAt   time.Time
}

type CandidateProfile struct {
	UserID            uint64 `gorm:"primaryKey"`
	Name              string `gorm:"size:64;not null"`
	Phone             string `gorm:"size:32;not null"`
	Education         string `gorm:"size:64;not null"`
	School            string `gorm:"size:128;not null"`
	Experience        string `gorm:"type:text;not null"`
	WorkExperience    string `gorm:"type:text"`
	ProjectExperience string `gorm:"type:text"`
	Skills            string `gorm:"size:512;not null"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Resume struct {
	ID          uint64 `gorm:"primaryKey"`
	UserID      uint64 `gorm:"not null;index;index:idx_resumes_user_status_created,priority:1"`
	FileName    string `gorm:"size:255;not null"`
	ObjectKey   string `gorm:"size:512;not null;uniqueIndex"`
	ContentType string `gorm:"size:128;not null"`
	Size        int64
	Status      string    `gorm:"size:32;not null;index;index:idx_resumes_user_status_created,priority:2"`
	CreatedAt   time.Time `gorm:"index:idx_resumes_user_status_created,priority:3"`
	UpdatedAt   time.Time
}

type Application struct {
	ID        uint64    `gorm:"primaryKey"`
	JobID     uint64    `gorm:"not null;uniqueIndex:idx_app_job_user;index;index:idx_apps_job_created,priority:1"`
	UserID    uint64    `gorm:"not null;uniqueIndex:idx_app_job_user;index;index:idx_apps_user_created,priority:1"`
	ResumeID  uint64    `gorm:"not null"`
	Status    string    `gorm:"size:32;not null;index"`
	CreatedAt time.Time `gorm:"index:idx_apps_job_created,priority:2;index:idx_apps_user_created,priority:2"`
	UpdatedAt time.Time
}

type ChatMessage struct {
	ID        uint64    `gorm:"primaryKey"`
	HRID      uint64    `gorm:"not null;index;index:idx_chat_messages_hr_created,priority:1"`
	Role      string    `gorm:"size:32;not null"`
	Content   string    `gorm:"type:text;not null"`
	CreatedAt time.Time `gorm:"index:idx_chat_messages_hr_created,priority:2"`
}
