<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { FileCheck2, RefreshCcw, Save, UploadCloud } from 'lucide-vue-next'
import { api, uploadResumeFile } from '../api/client'
import type { ApplicationDTO, CandidateProfileDTO } from '../types/api'

const profile = ref<CandidateProfileDTO>({
  name: '',
  phone: '',
  education: '',
  school: '',
  experience: '',
  work_experience: '',
  project_experience: '',
  skills: '',
})
const applications = ref<ApplicationDTO[]>([])
const profileLoading = ref(false)
const appLoading = ref(false)
const uploadLoading = ref(false)
const profileMessage = ref('')
const uploadMessage = ref('')
const error = ref('')

onMounted(() => {
  void loadProfile()
  void loadApplications()
})

async function loadProfile() {
  profileLoading.value = true
  error.value = ''
  try {
    const resp = await api.getProfile()
    if (resp.profile) {
      profile.value = { ...profile.value, ...resp.profile }
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : '个人档案加载失败'
  } finally {
    profileLoading.value = false
  }
}

async function saveProfile() {
  profileLoading.value = true
  profileMessage.value = ''
  error.value = ''
  try {
    const payload = {
      ...profile.value,
      experience: buildCombinedExperience(profile.value.work_experience, profile.value.project_experience),
    }
    const resp = await api.upsertProfile(payload)
    profile.value = { ...profile.value, ...resp.profile }
    profileMessage.value = '档案已保存'
  } catch (err) {
    error.value = err instanceof Error ? err.message : '档案保存失败'
  } finally {
    profileLoading.value = false
  }
}

function buildCombinedExperience(workExperience: string, projectExperience: string) {
  const parts: string[] = []
  if (workExperience.trim()) {
    parts.push(`工作经历\n${workExperience.trim()}`)
  }
  if (projectExperience.trim()) {
    parts.push(`项目经历\n${projectExperience.trim()}`)
  }
  return parts.join('\n\n')
}

async function uploadResume(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) {
    return
  }
  uploadLoading.value = true
  uploadMessage.value = ''
  error.value = ''
  try {
    const resume = await uploadResumeFile(file)
    uploadMessage.value = `简历已上传，编号 ${resume.id}`
    await loadApplications()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '简历上传失败'
  } finally {
    uploadLoading.value = false
    input.value = ''
  }
}

async function loadApplications() {
  appLoading.value = true
  error.value = ''
  try {
    const resp = await api.listMyApplications({ page: 1, page_size: 20 })
    applications.value = resp.items
  } catch (err) {
    error.value = err instanceof Error ? err.message : '投递记录加载失败'
  } finally {
    appLoading.value = false
  }
}
</script>

<template>
  <section class="workspace-grid">
    <div class="surface">
      <div class="section-head compact">
        <div>
          <p class="eyebrow">Candidate</p>
          <h2>候选人档案</h2>
        </div>
        <button class="ghost icon-text" type="button" @click="loadProfile">
          <RefreshCcw :size="16" />
          刷新
        </button>
      </div>

      <form class="form-grid two" @submit.prevent="saveProfile">
        <label>
          <span>姓名</span>
          <input v-model="profile.name" required placeholder="张三" />
        </label>
        <label>
          <span>电话</span>
          <input v-model="profile.phone" required placeholder="13800000000" />
        </label>
        <label>
          <span>学历</span>
          <input v-model="profile.education" required placeholder="本科" />
        </label>
        <label>
          <span>学校</span>
          <input v-model="profile.school" required placeholder="某某大学" />
        </label>
        <label class="span-2">
          <span>技能</span>
          <input v-model="profile.skills" required placeholder="Go, MySQL, gRPC" />
        </label>
        <label class="span-2">
          <span>工作经历</span>
          <textarea
            v-model="profile.work_experience"
            required
            rows="6"
            placeholder="公司 / 岗位 / 时间 / 负责内容 / 业务结果"
          />
        </label>
        <label class="span-2">
          <span>项目经历</span>
          <textarea
            v-model="profile.project_experience"
            required
            rows="6"
            placeholder="项目名称 / 技术栈 / 职责 / 难点 / 可量化成果"
          />
        </label>
        <button class="primary span-2" type="submit" :disabled="profileLoading">
          <Save :size="17" />
          {{ profileLoading ? '保存中...' : '保存档案' }}
        </button>
      </form>
      <p v-if="profileMessage" class="success">{{ profileMessage }}</p>
    </div>

    <div class="surface">
      <div class="section-head compact">
        <div>
          <p class="eyebrow">Resume</p>
          <h2>简历附件</h2>
        </div>
        <FileCheck2 :size="22" class="soft-icon" />
      </div>
      <label class="upload-box">
        <UploadCloud :size="28" />
        <span>{{ uploadLoading ? '上传中...' : '选择 PDF、DOC 或 DOCX 简历' }}</span>
        <input type="file" accept=".pdf,.doc,.docx" :disabled="uploadLoading" @change="uploadResume" />
      </label>
      <p v-if="uploadMessage" class="success">{{ uploadMessage }}</p>
      <p class="muted small">经历由上方表单填写，简历文件仅作为 HR 可下载的附件。</p>
    </div>

    <div class="surface span-wide">
      <div class="section-head compact">
        <div>
          <p class="eyebrow">Applications</p>
          <h2>我的投递</h2>
        </div>
        <button class="ghost icon-text" type="button" @click="loadApplications">
          <RefreshCcw :size="16" />
          刷新
        </button>
      </div>
      <p v-if="error" class="inline-error">{{ error }}</p>
      <div v-if="appLoading" class="empty">投递记录加载中...</div>
      <div v-else-if="applications.length === 0" class="empty">暂无投递记录</div>
      <div v-else class="table-list">
        <article v-for="app in applications" :key="app.id" class="row-card">
          <div>
            <strong>{{ app.job?.title ?? '岗位已不可用' }}</strong>
            <p class="muted">{{ app.job?.city }} · {{ app.job?.skills }}</p>
          </div>
          <span class="pill green">{{ app.status }}</span>
          <span class="muted">{{ app.created_at }}</span>
        </article>
      </div>
    </div>
  </section>
</template>
