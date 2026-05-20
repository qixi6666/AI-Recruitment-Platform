<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { BriefcaseBusiness, Search, Send } from 'lucide-vue-next'
import { api } from '../api/client'
import { useAuthStore } from '../stores/auth'
import type { JobDTO } from '../types/api'

const auth = useAuthStore()
const jobs = ref<JobDTO[]>([])
const total = ref(0)
const keyword = ref('')
const page = ref(1)
const pageSize = 8
const loading = ref(false)
const message = ref('')
const error = ref('')

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

onMounted(loadJobs)

async function loadJobs(nextPage = page.value) {
  loading.value = true
  error.value = ''
  page.value = nextPage
  try {
    const resp = await api.listPublicJobs({ page: page.value, page_size: pageSize, keyword: keyword.value.trim() })
    jobs.value = resp.items
    total.value = resp.total
  } catch (err) {
    error.value = err instanceof Error ? err.message : '岗位加载失败'
  } finally {
    loading.value = false
  }
}

async function apply(job: JobDTO) {
  message.value = ''
  error.value = ''
  if (auth.role !== 'candidate') {
    error.value = '请先以候选人身份登录'
    return
  }
  try {
    await api.applyJob(job.id)
    message.value = `已投递：${job.title}`
  } catch (err) {
    error.value = err instanceof Error ? err.message : '投递失败'
  }
}

function salary(job: JobDTO) {
  if (!job.salary_min && !job.salary_max) {
    return '薪资面议'
  }
  return `${job.salary_min || 0}-${job.salary_max || 0}`
}
</script>

<template>
  <section class="surface">
    <div class="section-head">
      <div>
        <p class="eyebrow">Open roles</p>
        <h2>公开岗位</h2>
      </div>
      <form class="search-bar" @submit.prevent="loadJobs(1)">
        <Search :size="18" />
        <input v-model="keyword" placeholder="搜索岗位、城市或技能" />
        <button class="ghost" type="submit">搜索</button>
      </form>
    </div>

    <p v-if="message" class="success">{{ message }}</p>
    <p v-if="error" class="inline-error">{{ error }}</p>

    <div v-if="loading" class="empty">岗位加载中...</div>
    <div v-else-if="jobs.length === 0" class="empty">暂无匹配岗位</div>
    <div v-else class="job-grid">
      <article v-for="job in jobs" :key="job.id" class="job-card">
        <div class="card-top">
          <BriefcaseBusiness :size="20" />
          <span :class="['pill', job.status === 'open' ? 'green' : 'gray']">{{ job.status }}</span>
        </div>
        <h3>{{ job.title }}</h3>
        <p class="muted">{{ job.city }} · {{ job.education || '学历不限' }} · {{ job.experience || '经验不限' }}</p>
        <p class="salary">{{ salary(job) }}</p>
        <p class="skills">{{ job.skills }}</p>
        <p class="description">{{ job.description }}</p>
        <button class="primary" type="button" @click="apply(job)">
          <Send :size="16" />
          投递
        </button>
      </article>
    </div>

    <div class="pager">
      <button class="ghost" type="button" :disabled="page <= 1" @click="loadJobs(page - 1)">上一页</button>
      <span>{{ page }} / {{ totalPages }}</span>
      <button class="ghost" type="button" :disabled="page >= totalPages" @click="loadJobs(page + 1)">下一页</button>
    </div>
  </section>
</template>
