<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Bot, SendHorizontal } from 'lucide-vue-next'
import { api, streamAIChat } from '../api/client'
import type { ChatMessageDTO } from '../types/api'

interface ChatLine {
  role: 'user' | 'assistant'
  content: string
  context?: Record<string, string>
}

const messages = ref<ChatLine[]>([])
const question = ref('')
const loading = ref(false)
const error = ref('')

onMounted(loadHistory)

async function loadHistory() {
  error.value = ''
  try {
    const resp = await api.listChatHistory(80)
    messages.value = resp.items.map(toLine)
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'AI 历史加载失败'
  }
}

async function ask() {
  const text = question.value.trim()
  if (!text || loading.value) {
    return
  }
  question.value = ''
  error.value = ''
  loading.value = true
  messages.value.push({ role: 'user', content: text })
  const assistant: ChatLine = { role: 'assistant', content: '' }
  messages.value.push(assistant)
  try {
    await streamAIChat(text, (chunk, event) => {
      if (event === 'delta') {
        assistant.content += chunk.content ?? ''
      }
      if (event === 'done') {
        assistant.context = chunk.context
      }
      if (event === 'error') {
        error.value = chunk.content ?? '流式对话失败'
      }
    })
    if (!assistant.content.trim()) {
      const resp = await api.aiChat(text)
      assistant.content = resp.answer
      assistant.context = resp.context
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'AI 对话失败'
    assistant.content = '这次对话没有成功返回。'
  } finally {
    loading.value = false
  }
}

function toLine(item: ChatMessageDTO): ChatLine {
  return {
    role: item.role === 'assistant' ? 'assistant' : 'user',
    content: item.content,
  }
}
</script>

<template>
  <section class="surface ai-panel">
    <div class="section-head compact">
      <div>
        <p class="eyebrow">AI Assistant</p>
        <h2>招聘数据问答</h2>
      </div>
      <Bot :size="24" class="soft-icon" />
    </div>

    <div class="chat-window">
      <div v-if="messages.length === 0" class="empty">可以询问岗位热度、候选人筛选或整体招聘概览。</div>
      <article v-for="(message, index) in messages" :key="index" :class="['chat-bubble', message.role]">
        <span>{{ message.role === 'user' ? '我' : 'AI' }}</span>
        <p>{{ message.content || '思考中...' }}</p>
        <small v-if="message.context?.used_tools">tools: {{ message.context.used_tools }}</small>
      </article>
    </div>

    <p v-if="error" class="inline-error">{{ error }}</p>
    <form class="chat-form" @submit.prevent="ask">
      <input v-model="question" placeholder="例如：哪个岗位投递最多？筛选 Go 技能候选人" />
      <button class="primary icon-only" type="submit" :disabled="loading" aria-label="发送">
        <SendHorizontal :size="18" />
      </button>
    </form>
  </section>
</template>
