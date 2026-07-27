<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Bot, Clock3, PlayCircle, RefreshCcw } from 'lucide-vue-next'
import { api } from '../api/client'
import type { LLMEvaluationResultDTO, LLMModelDTO } from '../types/api'

const models = ref<LLMModelDTO[]>([])
const selectedModels = ref<string[]>([])
const systemPrompt = ref('你是招聘系统的大模型评估助手，回答要准确、简洁、可复核。')
const prompt = ref('请从技术能力、项目经验、沟通协作三个角度，说明如何评估一名 Go 后端候选人。')
const temperature = ref(0)
const maxTokens = ref(512)
const loading = ref(false)
const modelLoading = ref(false)
const error = ref('')
const results = ref<LLMEvaluationResultDTO[]>([])

const configuredModels = computed(() => models.value.filter((model) => model.configured))
const canEvaluate = computed(() => selectedModels.value.length > 0 && prompt.value.trim() !== '' && !loading.value)

onMounted(() => {
  void loadModels()
})

async function loadModels() {
  modelLoading.value = true
  error.value = ''
  try {
    const resp = await api.listLLMModels()
    models.value = resp.models
    selectedModels.value = resp.models.filter((model) => model.configured).map((model) => model.name)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '模型列表加载失败'
  } finally {
    modelLoading.value = false
  }
}

async function evaluate() {
  if (!canEvaluate.value) {
    return
  }
  loading.value = true
  error.value = ''
  results.value = []
  try {
    const resp = await api.evaluateLLM({
      models: selectedModels.value,
      system_prompt: systemPrompt.value.trim(),
      prompt: prompt.value.trim(),
      temperature: temperature.value,
      max_tokens: maxTokens.value,
    })
    results.value = resp.results
  } catch (err) {
    error.value = err instanceof Error ? err.message : '模型评估失败'
  } finally {
    loading.value = false
  }
}

function usageText(result: LLMEvaluationResultDTO) {
  if (!result.usage?.total_tokens) {
    return 'token 未返回'
  }
  return `${result.usage.total_tokens} tokens`
}
</script>

<template>
  <div class="surface llm-panel">
    <div class="section-head compact">
      <div>
        <p class="eyebrow">LLM Gateway</p>
        <h2>模型评估</h2>
      </div>
      <div class="icon-text muted">
        <Bot :size="22" />
        <span>{{ configuredModels.length }}/{{ models.length }} 可调用</span>
      </div>
    </div>

    <form class="llm-form" @submit.prevent="evaluate">
      <div class="llm-model-grid">
        <label v-for="model in models" :key="model.name" class="model-option">
          <input v-model="selectedModels" type="checkbox" :value="model.name" :disabled="!model.configured || loading" />
          <span>
            <strong>{{ model.name }}</strong>
            <small>{{ model.provider }} · {{ model.model }}</small>
          </span>
          <em :class="['pill', model.configured ? 'green' : 'gray']">
            {{ model.configured ? 'ready' : 'missing key' }}
          </em>
        </label>
        <div v-if="modelLoading" class="empty span-2">模型加载中...</div>
        <div v-else-if="models.length === 0" class="empty span-2">暂无模型配置</div>
      </div>

      <div class="form-grid two">
        <label class="span-2">
          <span>系统提示词</span>
          <textarea v-model="systemPrompt" rows="2" :disabled="loading" />
        </label>
        <label class="span-2">
          <span>评估问题</span>
          <textarea v-model="prompt" rows="4" required :disabled="loading" />
        </label>
        <label>
          <span>Temperature</span>
          <input v-model.number="temperature" min="0" max="2" step="0.1" type="number" :disabled="loading" />
        </label>
        <label>
          <span>Max Tokens</span>
          <input v-model.number="maxTokens" min="1" max="8192" type="number" :disabled="loading" />
        </label>
      </div>

      <div class="llm-actions">
        <button class="ghost icon-text" type="button" :disabled="modelLoading || loading" @click="loadModels">
          <RefreshCcw :size="16" />
          刷新模型
        </button>
        <button class="primary icon-text" type="submit" :disabled="!canEvaluate">
          <PlayCircle :size="17" />
          {{ loading ? '评估中...' : '开始评估' }}
        </button>
      </div>
    </form>

    <p v-if="error" class="inline-error">{{ error }}</p>

    <div v-if="results.length > 0" class="llm-result-grid">
      <article v-for="result in results" :key="result.model" class="llm-result-card">
        <div class="card-top">
          <div>
            <strong>{{ result.model }}</strong>
            <p class="muted">{{ result.provider }}</p>
          </div>
          <span class="pill green">
            <Clock3 :size="13" />
            {{ result.latency_ms }}ms
          </span>
        </div>
        <p class="muted small">{{ usageText(result) }} · {{ result.finish_reason || 'no finish reason' }}</p>
        <p v-if="result.error" class="inline-error">{{ result.error }}</p>
        <pre v-else class="llm-output">{{ result.content }}</pre>
      </article>
    </div>
  </div>
</template>
