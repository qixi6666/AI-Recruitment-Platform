<script setup lang="ts">
import { computed, ref } from 'vue'
import { Search, Sparkles } from 'lucide-vue-next'
import { streamResumeRecommendations } from '../api/client'
import type { JobDTO, ResumeRecommendationResponse } from '../types/api'

const props = defineProps<{
  jobs: JobDTO[]
}>()

const selectedJobId = ref('')
const jobDescription = ref('')
const hrRequirements = ref('')
const limit = ref(10)
const evidenceLimit = ref(3)
const loading = ref(false)
const error = ref('')
const progress = ref('')
const taskID = ref('')
const finalAnswerText = ref('')
const result = ref<ResumeRecommendationResponse | null>(null)

const openJobs = computed(() => props.jobs.filter((job) => job.status === 'open'))

async function recommend() {
  const jobID = Number(selectedJobId.value)
  const jd = jobDescription.value.trim()
  const requirements = hrRequirements.value.trim()
  if (!jobID && !jd && !requirements) {
    error.value = '请选择岗位、粘贴 JD 或填写补充要求'
    return
  }
  loading.value = true
  error.value = ''
  progress.value = '思考中，正在提炼 JD 和补充要求'
  taskID.value = ''
  finalAnswerText.value = ''
  result.value = null
  const payload = {
    job_id: jobID > 0 ? jobID : undefined,
    job_description: jd || undefined,
    queries: requirements ? [requirements] : undefined,
    limit: limit.value,
    evidence_limit: evidenceLimit.value,
  }
  try {
    const task = await streamResumeRecommendations(payload, (chunk) => {
      if (chunk.task_id) {
        taskID.value = chunk.task_id
      }
      if (chunk.stage === 'final_answer_streaming') {
        finalAnswerText.value += chunk.message
        progress.value = '最终推荐回答正在生成'
      } else {
        progress.value = chunk.message
      }
      if (chunk.done && chunk.response) {
        result.value = chunk.response
      }
    })
    taskID.value = task.task_id
    if (!result.value) {
      error.value = '推荐任务已结束，但没有返回候选人结果'
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : '简历推荐失败'
  } finally {
    loading.value = false
  }
}

function percent(value: number) {
  return `${Math.round(value * 100)}`
}
</script>

<template>
  <section class="surface recommendation-panel">
    <div class="section-head compact">
      <div>
        <p class="eyebrow">Resume Match</p>
        <h2>简历推荐</h2>
      </div>
      <Sparkles :size="24" class="soft-icon" />
    </div>

    <form class="recommend-form" :aria-busy="loading" @submit.prevent="recommend">
      <fieldset class="recommend-fieldset" :disabled="loading">
        <label>
          <span>岗位</span>
          <select v-model="selectedJobId">
            <option value="">不限定岗位，使用下方 JD</option>
            <option v-for="job in openJobs" :key="job.id" :value="String(job.id)">
              {{ job.title }} · {{ job.city }}
            </option>
          </select>
        </label>
        <label>
          <span>推荐人数</span>
          <input v-model.number="limit" min="1" max="10" type="number" />
        </label>
        <label>
          <span>证据数</span>
          <input v-model.number="evidenceLimit" min="1" max="5" type="number" />
        </label>
        <label class="span-3">
          <span>JD</span>
          <textarea
            v-model="jobDescription"
            rows="4"
            placeholder="可直接粘贴岗位 JD；如果已选择岗位，留空则使用岗位描述"
          />
        </label>
        <label class="span-3">
          <span>补充要求</span>
          <textarea
            v-model="hrRequirements"
            rows="3"
            placeholder="例如：优先有支付系统经验，必须熟悉 Go，最好能接受一周内到岗"
          />
        </label>
        <button class="primary span-3" type="submit" :disabled="loading">
          <Search :size="17" />
          {{ loading ? '思考中...' : '生成推荐' }}
        </button>
      </fieldset>
    </form>

    <p v-if="error" class="inline-error">{{ error }}</p>
    <p v-if="taskID && !result" class="muted">任务 {{ taskID }} · {{ progress || '等待推荐结果' }}</p>
    <p v-else-if="loading && progress" class="muted">{{ progress }}</p>
    <pre v-if="finalAnswerText && !result" class="final-answer-stream">{{ finalAnswerText }}</pre>
    <p v-if="result?.scope" class="muted">
      {{ result.scope }}
      <span v-if="result.agent_status === 'rag_only'"> · 已降级为 RAG 排序</span>
    </p>
    <p v-if="result?.fallback_reason" class="inline-error">{{ result.fallback_reason }}</p>

    <div v-if="result && result.candidates.length === 0" class="empty">暂无匹配候选人</div>
    <div v-else-if="result" class="recommendation-list">
      <article v-for="candidate in result.candidates" :key="candidate.candidate_id" class="recommendation-card">
        <div class="card-top">
          <div>
            <strong>{{ candidate.candidate_name || `候选人 #${candidate.candidate_id}` }}</strong>
            <p class="muted">{{ candidate.job_title }} · {{ candidate.resume_name }}</p>
          </div>
          <span class="pill green">{{ percent(candidate.score) }}分</span>
        </div>
        <div class="score-row">
          <span>语义 {{ percent(candidate.semantic_score ?? 0) }}</span>
          <span>关键词 {{ percent(candidate.keyword_score ?? 0) }}</span>
        </div>
        <p v-if="candidate.reason" class="recommendation-reason">{{ candidate.reason }}</p>
        <ul v-if="candidate.risk_points?.length" class="risk-list">
          <li v-for="risk in candidate.risk_points" :key="risk">{{ risk }}</li>
        </ul>
        <div class="evidence-list">
          <section v-for="item in candidate.evidence" :key="item.chunk_id" class="evidence-item">
            <strong>{{ item.section_title || item.section_type }}</strong>
            <p>{{ item.content }}</p>
          </section>
        </div>
      </article>
    </div>
  </section>
</template>
