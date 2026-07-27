export type Role = 'hr' | 'candidate'

export interface UserDTO {
  id: number
  username: string
  role: Role
  created_at?: string
}

export interface JobDTO {
  id: number
  hr_id: number
  title: string
  city: string
  salary_min: number
  salary_max: number
  education: string
  experience: string
  skills: string
  description: string
  status: 'open' | 'offline' | string
  application_num: number
  created_at: string
  updated_at: string
}

export interface CandidateProfileDTO {
  user_id?: number
  name: string
  phone: string
  education: string
  school: string
  experience: string
  work_experience: string
  project_experience: string
  skills: string
  updated_at?: string
}

export interface ResumeDTO {
  id: number
  user_id: number
  file_name: string
  object_key: string
  file_path: string
  content_type: string
  size: number
  status: string
  download_url?: string
  created_at: string
}

export interface ApplicationDTO {
  id: number
  job?: JobDTO
  candidate?: UserDTO
  profile?: CandidateProfileDTO
  resume?: ResumeDTO
  status: string
  created_at: string
}

export interface ListResponse<T> {
  items: T[]
  total: number
}

export interface ResumeRecommendationEvidence {
  score: number
  semantic_score?: number
  keyword_score?: number
  chunk_id: number
  resume_id: number
  job_id: number
  job_title: string
  candidate_id: number
  candidate_name: string
  section_type: string
  section_title: string
  experience_index: number
  chunk_index: number
  content: string
  resume_name: string
}

export interface ResumeRecommendationCandidate {
  candidate_id: number
  candidate_name: string
  resume_id: number
  resume_name: string
  job_id: number
  job_title: string
  score: number
  semantic_score?: number
  keyword_score?: number
  reason?: string
  risk_points?: string[]
  evidence: ResumeRecommendationEvidence[]
}

export interface ResumeRecommendationResponse {
  scope: string
  job_id?: number
  job_title?: string
  agent_status: string
  fallback_reason?: string
  candidates: ResumeRecommendationCandidate[]
}

export interface ResumeRecommendationTaskResponse {
  task_id: string
  status: string
  created_at?: string
  stream_url?: string
  cached?: boolean
  response?: ResumeRecommendationResponse
}

export interface ResumeRecommendationStreamChunk {
  task_id?: string
  status?: string
  stage: string
  message: string
  done: boolean
  response?: ResumeRecommendationResponse
}

export interface LLMModelDTO {
  name: string
  provider: string
  base_url: string
  model: string
  configured: boolean
}

export interface LLMUsageDTO {
  prompt_tokens?: number
  completion_tokens?: number
  total_tokens?: number
}

export interface LLMEvaluationResultDTO {
  model: string
  provider: string
  content?: string
  finish_reason?: string
  latency_ms: number
  usage?: LLMUsageDTO
  error?: string
}

export interface LLMEvaluateResponse {
  results: LLMEvaluationResultDTO[]
}

export interface ApiEnvelope<T> {
  data: T
}
