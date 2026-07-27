import type {
  ApiEnvelope,
  ApplicationDTO,
  CandidateProfileDTO,
  JobDTO,
  LLMEvaluateResponse,
  LLMModelDTO,
  ListResponse,
  ResumeRecommendationStreamChunk,
  ResumeRecommendationTaskResponse,
  Role,
  ResumeDTO,
  UserDTO,
} from '../types/api'

const API_PREFIX = '/api/v1'

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
  }
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers)
  const token = localStorage.getItem('recruitment_token')
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  if (options.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  const response = await fetch(`${API_PREFIX}${path}`, {
    ...options,
    headers,
  })

  const contentType = response.headers.get('content-type') ?? ''
  const payload = contentType.includes('application/json') ? await response.json() : null

  if (!response.ok) {
    const message = payload?.error ?? `HTTP ${response.status}`
    throw new ApiError(message, response.status)
  }

  return (payload as ApiEnvelope<T>).data
}

function query(params: Record<string, string | number | undefined>) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== '') {
      search.set(key, String(value))
    }
  })
  const text = search.toString()
  return text ? `?${text}` : ''
}

export interface LoginPayload {
  username: string
  password: string
  role: Role
}

export interface LoginResult {
  token: string
  user: UserDTO
}

export interface JobPayload {
  title: string
  city: string
  salary_min: number
  salary_max: number
  education: string
  experience: string
  skills: string
  description: string
}

export const api = {
  register(payload: LoginPayload) {
    return request<{ user: UserDTO }>('/auth/register', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  login(payload: LoginPayload) {
    return request<LoginResult>('/auth/login', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  listPublicJobs(params: { page: number; page_size: number; keyword?: string }) {
    return request<ListResponse<JobDTO>>(`/jobs${query(params)}`)
  },
  applyJob(id: number) {
    return request<{ application: ApplicationDTO }>(`/jobs/${id}/apply`, { method: 'POST' })
  },
  getProfile() {
    return request<{ profile?: CandidateProfileDTO }>('/me/profile')
  },
  upsertProfile(payload: CandidateProfileDTO) {
    return request<{ profile: CandidateProfileDTO }>('/me/profile', {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  listMyApplications(params: { page: number; page_size: number }) {
    return request<ListResponse<ApplicationDTO>>(`/me/applications${query(params)}`)
  },
  listHRJobs(params: { page: number; page_size: number; status?: string; keyword?: string }) {
    return request<ListResponse<JobDTO>>(`/hr/jobs${query(params)}`)
  },
  createJob(payload: JobPayload) {
    return request<{ job: JobDTO }>('/hr/jobs', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  updateJob(id: number, payload: JobPayload) {
    return request<{ job: JobDTO }>(`/hr/jobs/${id}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    })
  },
  offlineJob(id: number) {
    return request<{ job: JobDTO }>(`/hr/jobs/${id}/offline`, { method: 'PATCH' })
  },
  listHRApplications(params: { page: number; page_size: number; job_id?: number }) {
    return request<ListResponse<ApplicationDTO>>(`/hr/applications${query(params)}`)
  },
  recommendResumes(payload: {
    job_id?: number
    job_description?: string
    queries?: string[]
    limit?: number
    evidence_limit?: number
  }) {
    return request<ResumeRecommendationTaskResponse>('/hr/resume-recommendations', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
  listLLMModels() {
    return request<{ models: LLMModelDTO[] }>('/hr/llm/models')
  },
  evaluateLLM(payload: {
    models?: string[]
    system_prompt?: string
    prompt?: string
    temperature?: number
    max_tokens?: number
  }) {
    return request<LLMEvaluateResponse>('/hr/llm/evaluate', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
  },
}

export async function uploadResumeFile(file: File): Promise<ResumeDTO> {
  const form = new FormData()
  form.set('file', file)
  const token = localStorage.getItem('recruitment_token')
  const response = await fetch(`${API_PREFIX}/me/resume/upload`, {
    method: 'POST',
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
    body: form,
  })
  const payload = await response.json()
  if (!response.ok) {
    throw new ApiError(payload?.error ?? '简历上传失败', response.status)
  }
  return payload.data.resume as ResumeDTO
}

export async function downloadResume(resume: ResumeDTO) {
  const token = localStorage.getItem('recruitment_token')
  const response = await fetch(`${API_PREFIX}/hr/resumes/${resume.id}/download`, {
    headers: token ? { Authorization: `Bearer ${token}` } : undefined,
  })
  if (!response.ok) {
    let message = `HTTP ${response.status}`
    try {
      message = (await response.json()).error ?? message
    } catch {
      // Keep the transport error when the response is not JSON.
    }
    throw new ApiError(message, response.status)
  }
  const blob = await response.blob()
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = resume.file_name || 'resume'
  link.click()
  URL.revokeObjectURL(url)
}

export async function streamResumeRecommendations(
  payload: {
    job_id?: number
    job_description?: string
    queries?: string[]
    limit?: number
    evidence_limit?: number
  },
  onChunk: (chunk: ResumeRecommendationStreamChunk, event: string) => void,
) {
  const task = await api.recommendResumes(payload)
  if (task.cached && task.response) {
    onChunk(
      {
        task_id: task.task_id,
        status: task.status,
        stage: 'done',
        message: '命中缓存，已返回上次推荐结果',
        done: true,
        response: task.response,
      },
      'done',
    )
    return task
  }
  onChunk(
    {
      task_id: task.task_id,
      status: task.status,
      stage: 'queued',
      message: '推荐任务已提交，等待最终推荐回答',
      done: false,
    },
    'progress',
  )
  await streamResumeRecommendationTask(task.task_id, onChunk)
  return task
}

export async function streamResumeRecommendationTask(
  taskID: string,
  onChunk: (chunk: ResumeRecommendationStreamChunk, event: string) => void,
) {
  const token = localStorage.getItem('recruitment_token')
  const response = await fetch(`${API_PREFIX}/hr/resume-recommendations/${taskID}/stream`, {
    method: 'GET',
    headers: {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
  })
  if (!response.ok || !response.body) {
    let message = `HTTP ${response.status}`
    try {
      message = (await response.json()).error ?? message
    } catch {
      // Keep the transport error when the response is not JSON.
    }
    throw new ApiError(message, response.status)
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  while (true) {
    const { value, done } = await reader.read()
    if (done) {
      break
    }
    buffer += decoder.decode(value, { stream: true })
    const frames = buffer.split('\n\n')
    buffer = frames.pop() ?? ''
    frames.forEach((frame) => {
      const lines = frame.split('\n')
      const event = lines.find((line) => line.startsWith('event:'))?.slice(6).trim() ?? 'message'
      const data = lines.find((line) => line.startsWith('data:'))?.slice(5).trim()
      if (!data) {
        return
      }
      onChunk(JSON.parse(data) as ResumeRecommendationStreamChunk, event)
    })
  }
}
