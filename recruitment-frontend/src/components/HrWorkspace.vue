<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Archive, Edit3, Plus, RefreshCcw, Save, UsersRound } from 'lucide-vue-next'
import { api, downloadResume, type JobPayload } from '../api/client'
import ResumeRecommendations from './ResumeRecommendations.vue'
import type { ApplicationDTO, JobDTO } from '../types/api'

const blankJob = (): JobPayload => ({
  title: '',
  city: '',
  salary_min: 0,
  salary_max: 0,
  education: '',
  experience: '',
  skills: '',
  description: '',
})

const jobs = ref<JobDTO[]>([])
const applications = ref<ApplicationDTO[]>([])
const form = ref<JobPayload>(blankJob())
const editingId = ref<number | null>(null)
const status = ref('')
const keyword = ref('')
const selectedJobId = ref('')
const loading = ref(false)
const appLoading = ref(false)
const message = ref('')
const error = ref('')

const openJobs = computed(() => jobs.value.filter((job) => job.status === 'open').length)
const applicationTotal = computed(() => applications.value.length)
const selectedJobIDNumber = computed(() => {
  const parsed = Number(selectedJobId.value)
  return parsed > 0 ? parsed : undefined
})

onMounted(() => {
  void loadJobs()
  void loadApplications()
})

async function loadJobs() {
  loading.value = true
  error.value = ''
  try {
    const resp = await api.listHRJobs({
      page: 1,
      page_size: 50,
      status: status.value,
      keyword: keyword.value.trim(),
    })
    jobs.value = resp.items
  } catch (err) {
    error.value = err instanceof Error ? err.message : '岗位加载失败'
  } finally {
    loading.value = false
  }
}

async function saveJob() {
  loading.value = true
  message.value = ''
  error.value = ''
  try {
    if (editingId.value) {
      await api.updateJob(editingId.value, form.value)
      message.value = '岗位已更新'
    } else {
      await api.createJob(form.value)
      message.value = '岗位已创建'
    }
    resetForm()
    await loadJobs()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '岗位保存失败'
  } finally {
    loading.value = false
  }
}

async function offline(job: JobDTO) {
  error.value = ''
  message.value = ''
  try {
    await api.offlineJob(job.id)
    message.value = `已下架：${job.title}`
    await loadJobs()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '下架失败'
  }
}

function edit(job: JobDTO) {
  editingId.value = job.id
  form.value = {
    title: job.title,
    city: job.city,
    salary_min: job.salary_min,
    salary_max: job.salary_max,
    education: job.education,
    experience: job.experience,
    skills: job.skills,
    description: job.description,
  }
}

function resetForm() {
  editingId.value = null
  form.value = blankJob()
}

async function loadApplications() {
  appLoading.value = true
  error.value = ''
  try {
    const resp = await api.listHRApplications({
      page: 1,
      page_size: 50,
      job_id: selectedJobIDNumber.value,
    })
    applications.value = resp.items
  } catch (err) {
    error.value = err instanceof Error ? err.message : '投递台账加载失败'
  } finally {
    appLoading.value = false
  }
}

async function handleDownload(app: ApplicationDTO) {
  if (!app.resume) {
    return
  }
  error.value = ''
  try {
    await downloadResume(app.resume)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '简历下载失败'
  }
}
</script>

<template>
  <section class="workspace-grid hr-layout">
    <div class="stat-strip span-wide">
      <article>
        <span>岗位总数</span>
        <strong>{{ jobs.length }}</strong>
      </article>
      <article>
        <span>开放岗位</span>
        <strong>{{ openJobs }}</strong>
      </article>
      <article>
        <span>当前投递</span>
        <strong>{{ applicationTotal }}</strong>
      </article>
    </div>

    <div class="surface">
      <div class="section-head compact">
        <div>
          <p class="eyebrow">Job Editor</p>
          <h2>{{ editingId ? '编辑岗位' : '新增岗位' }}</h2>
        </div>
        <button class="ghost icon-text" type="button" @click="resetForm">
          <Plus :size="16" />
          新建
        </button>
      </div>
      <form class="form-grid two" @submit.prevent="saveJob">
        <label>
          <span>标题</span>
          <input v-model="form.title" required placeholder="Go 后端工程师" />
        </label>
        <label>
          <span>城市</span>
          <input v-model="form.city" required placeholder="上海" />
        </label>
        <label>
          <span>最低薪资</span>
          <input v-model.number="form.salary_min" min="0" type="number" />
        </label>
        <label>
          <span>最高薪资</span>
          <input v-model.number="form.salary_max" min="0" type="number" />
        </label>
        <label>
          <span>学历</span>
          <input v-model="form.education" placeholder="本科" />
        </label>
        <label>
          <span>经验</span>
          <input v-model="form.experience" placeholder="3-5年" />
        </label>
        <label class="span-2">
          <span>技能</span>
          <input v-model="form.skills" placeholder="Go, Gin, gRPC, MySQL" />
        </label>
        <label class="span-2">
          <span>描述</span>
          <textarea v-model="form.description" rows="5" placeholder="负责招聘系统后端服务研发" />
        </label>
        <button class="primary span-2" type="submit" :disabled="loading">
          <Save :size="17" />
          {{ loading ? '保存中...' : '保存岗位' }}
        </button>
      </form>
    </div>

    <div class="surface">
      <div class="section-head compact">
        <div>
          <p class="eyebrow">My Jobs</p>
          <h2>岗位管理</h2>
        </div>
        <button class="ghost icon-text" type="button" @click="loadJobs">
          <RefreshCcw :size="16" />
          刷新
        </button>
      </div>
      <form class="toolbar" @submit.prevent="loadJobs">
        <input v-model="keyword" placeholder="搜索技能或标题" />
        <select v-model="status">
          <option value="">全部状态</option>
          <option value="open">开放</option>
          <option value="offline">下架</option>
        </select>
        <button class="ghost" type="submit">筛选</button>
      </form>
      <p v-if="message" class="success">{{ message }}</p>
      <p v-if="error" class="inline-error">{{ error }}</p>
      <div v-if="loading" class="empty">岗位加载中...</div>
      <div v-else class="table-list">
        <article v-for="job in jobs" :key="job.id" class="row-card job-row">
          <div>
            <strong>{{ job.title }}</strong>
            <p class="muted">{{ job.city }} · {{ job.skills }}</p>
          </div>
          <span :class="['pill', job.status === 'open' ? 'green' : 'gray']">{{ job.status }}</span>
          <button class="ghost icon-only" type="button" aria-label="编辑" @click="edit(job)">
            <Edit3 :size="16" />
          </button>
          <button
            class="ghost icon-only"
            type="button"
            aria-label="下架"
            :disabled="job.status !== 'open'"
            @click="offline(job)"
          >
            <Archive :size="16" />
          </button>
        </article>
      </div>
    </div>

    <div class="surface span-wide">
      <div class="section-head compact">
        <div>
          <p class="eyebrow">Ledger</p>
          <h2>投递台账</h2>
        </div>
        <UsersRound :size="23" class="soft-icon" />
      </div>
      <form class="toolbar" @submit.prevent="loadApplications">
        <select v-model="selectedJobId">
          <option value="">全部岗位</option>
          <option v-for="job in jobs" :key="job.id" :value="String(job.id)">{{ job.title }}</option>
        </select>
        <button class="ghost" type="submit">查看</button>
      </form>
      <div v-if="appLoading" class="empty">投递台账加载中...</div>
      <div v-else-if="applications.length === 0" class="empty">暂无投递</div>
      <div v-else class="application-grid">
        <article v-for="app in applications" :key="app.id" class="application-card">
          <div class="card-top">
            <strong>{{ app.profile?.name ?? app.candidate?.username }}</strong>
            <span class="pill green">{{ app.status }}</span>
          </div>
          <p class="muted">{{ app.job?.title }} · {{ app.job?.city }}</p>
          <p>{{ app.profile?.skills }}</p>
          <p class="muted">{{ app.profile?.education }} · {{ app.profile?.school }} · {{ app.profile?.phone }}</p>
          <p v-if="app.profile?.work_experience" class="experience-preview">
            工作：{{ app.profile.work_experience }}
          </p>
          <p v-if="app.profile?.project_experience" class="experience-preview">
            项目：{{ app.profile.project_experience }}
          </p>
          <button v-if="app.resume" class="link-button" type="button" @click="handleDownload(app)">
            下载简历
          </button>
        </article>
      </div>
    </div>

    <ResumeRecommendations class="span-wide" :jobs="jobs" />
  </section>
</template>
