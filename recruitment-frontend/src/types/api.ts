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

export interface ChatMessageDTO {
  id: number
  hr_id: number
  role: 'user' | 'assistant' | string
  content: string
  created_at: string
}

export interface AIChatResponse {
  answer: string
  context: Record<string, string>
}

export interface AIChatStreamChunk {
  content?: string
  done?: boolean
  context?: Record<string, string>
}

export interface ApiEnvelope<T> {
  data: T
}
